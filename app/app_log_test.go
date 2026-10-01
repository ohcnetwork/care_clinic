package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/applog"
)

func loggingApp(t *testing.T) (*App, func() string) {
	t.Helper()
	a := roleApp(t)
	a.log = applog.Open()
	t.Cleanup(a.log.Close)
	path := a.log.Path()
	if path == "" {
		t.Skip("this computer has no writable log folder")
	}
	return a, func() string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
}

// Nothing the interface shows may be missing from the log, and nothing may be
// in it twice: the operator reads this file to explain a failure to support.
func TestBoundMethodErrorsAreLoggedOnceUnderTheirOwnName(t *testing.T) {
	a, read := loggingApp(t)
	before := read()

	// Runs through withJob, which logs for every method that uses it.
	if err := a.ConnectClient("not a clinic address"); err == nil {
		t.Fatal("accepted an invalid clinic address")
	}
	// Takes no job lock, so it carries the deferred wrapper itself.
	if err := a.SetMDNSName("Not A Name"); err == nil {
		t.Fatal("accepted an invalid clinic address")
	}
	added := strings.TrimPrefix(read(), before)
	for _, method := range []string{"ConnectClient:", "SetMDNSName:"} {
		if strings.Count(added, method) != 1 {
			t.Fatalf("%s appears %d times in the log:\n%s", method, strings.Count(added, method), added)
		}
	}
}

// A method that calls another bound method must not log the same failure twice.
func TestAnAlreadyLoggedErrorIsNotWrittenAgain(t *testing.T) {
	a, read := loggingApp(t)
	before := read()
	first := a.logged(errors.New("the clinic could not be reached"))
	if a.logged(first) != first {
		t.Fatal("logging an error twice replaced it")
	}
	added := strings.TrimPrefix(read(), before)
	if strings.Count(added, "the clinic could not be reached") != 1 {
		t.Fatalf("the same failure was written twice:\n%s", added)
	}
	var already loggedError
	if !errors.As(first, &already) || first.Error() != "the clinic could not be reached" {
		t.Fatalf("logging changed what the interface is told: %v", first)
	}
}

func TestBoundMethodNamesTheExportedCaller(t *testing.T) {
	a := roleApp(t)
	if got := a.NamedForTest(); got != "NamedForTest" {
		t.Fatalf("boundMethod = %q", got)
	}
	if got := boundMethod(); got != "CARE Desktop" {
		t.Fatalf("a caller that is not a bound method should be named generically: %q", got)
	}
}

// NamedForTest stands in for a bound method; boundMethod has to walk out of the
// unexported helpers it is called from to find it. It exists only in the test
// build, so it never becomes part of the interface's API.
func (a *App) NamedForTest() string {
	return a.helperForTest()
}

func (a *App) helperForTest() string { return boundMethod() }

func TestLogFolderIsNotTheInstallDir(t *testing.T) {
	a := roleApp(t)
	if strings.HasPrefix(applog.DefaultLogDir(), filepath.Join(a.installDir(), "")) {
		t.Fatal("the log would be deleted with the installation")
	}
}
