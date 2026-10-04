package hosts

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

func TestHasEntryRequiresLocalAddress(t *testing.T) {
	for _, tc := range []struct {
		data string
		want bool
	}{
		{"127.0.0.1 care.local # care-clinic\n", true},
		{"::1 localhost CARE.LOCAL\n", true},
		{"127.0.0.1 localhost care.local # other\n", true},
		{"192.0.2.1 care.local\n", false},
		{"127.0.0.1 care.local\n192.0.2.1 care.local\n", false},
		{"127.0.0.1 other.local # care.local\n", false},
		{"# 127.0.0.1 care.local\n", false},
		{"care.local\n", false},
	} {
		if got := hasEntry(tc.data, "care.local"); got != tc.want {
			t.Errorf("hasEntry(%q) = %v, want %v", tc.data, got, tc.want)
		}
	}
}

func TestReplaceStepPreservesUnownedLinesAndHandlesEmptyResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell fixture")
	}
	for _, tc := range []struct {
		name string
		data string
		keep func(string) (string, bool)
		want string
	}{
		{"mixed", "127.0.0.1 localhost\n" + line("care.local") + "\n", withoutMarker, "127.0.0.1 localhost\n"},
		{"owned only", line("care.local") + "\n", withoutMarker, ""},
		{"unowned clinic name", "127.0.0.1 localhost\n10.0.0.5 care.local\n",
			func(d string) (string, bool) { return withoutHost(d, "care.local") }, "127.0.0.1 localhost\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "hosts file")
			if err := os.WriteFile(p, []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			step, cleanup, need, err := replaceStep(p, tc.keep, "test")
			if err != nil || !need {
				t.Fatalf("replaceStep: need = %v, err = %v", need, err)
			}
			if out, err := proc.Command("sh", "-c", step.Sh).CombinedOutput(); err != nil {
				t.Fatalf("replace: %v: %s", err, out)
			}
			cleanup()
			data, err := os.ReadFile(p)
			if err != nil || string(data) != tc.want {
				t.Fatalf("remaining hosts = %q, %v; want %q", data, err, tc.want)
			}
			backup, err := os.ReadFile(p + ".care-backup")
			if err != nil || string(backup) != tc.data {
				t.Fatalf("backup = %q, %v; want %q", backup, err, tc.data)
			}
		})
	}
}

func TestReplaceStepSkipsCleanOrMissingFile(t *testing.T) {
	dir := t.TempDir()
	if _, _, need, err := replaceStep(filepath.Join(dir, "missing"), withoutMarker, "test"); need || err != nil {
		t.Fatalf("missing file: need = %v, err = %v", need, err)
	}
	p := filepath.Join(dir, "hosts")
	if err := os.WriteFile(p, []byte("127.0.0.1 localhost\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, need, err := replaceStep(p, withoutMarker, "test"); need || err != nil {
		t.Fatalf("clean file: need = %v, err = %v", need, err)
	}
}

func TestReplaceScriptPreservesWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell fixture")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "hosts")
	if err := os.WriteFile(p, []byte("127.0.0.1 localhost\n"+line("care.local")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	step, cleanup, _, err := replaceStep(p, withoutMarker, "test")
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cat"), []byte("#!/bin/sh\nexit 47\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := proc.Command("sh", "-c", step.Sh).CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 47 {
		t.Fatalf("write failure was lost: %v, %s", err, out)
	}
}

func TestRemoveUnixUsesRemainingState(t *testing.T) {
	for _, tc := range []struct {
		name             string
		removedInitially bool
		approve          bool
		removeElevated   bool
		wantCalls        []bool
		wantLeftover     bool
	}{
		{"successful command left entry", false, true, true, []bool{false, true}, false},
		{"failed command removed entry", true, true, false, []bool{false}, false},
		{"elevation left entry", false, true, false, []bool{false, true}, true},
		{"approval declined", false, false, false, []bool{false}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "hosts")
			if err := os.WriteFile(p, []byte(line("care.local")+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			var calls []bool
			got := removeUnix(func(string, string) bool { return tc.approve }, "care.local", p,
				func(_ string, elevated bool) error {
					calls = append(calls, elevated)
					if !elevated && tc.removedInitially || elevated && tc.removeElevated {
						if err := os.WriteFile(p, nil, 0o600); err != nil {
							t.Fatal(err)
						}
					}
					if tc.removedInitially {
						return errors.New("command reported a failure")
					}
					return nil
				})
			if !slices.Equal(calls, tc.wantCalls) || (got != "") != tc.wantLeftover {
				t.Fatalf("calls = %v, leftover = %q", calls, got)
			}
		})
	}
}

func TestLeftoverDoesNotTreatReadFailureAsRemoval(t *testing.T) {
	if got := leftover("care.local", t.TempDir()); !strings.Contains(got, "Could not check") {
		t.Fatalf("read failure became successful removal: %q", got)
	}
}

func TestInspectDistinguishesAbsentFromUnreadable(t *testing.T) {
	dir := t.TempDir()
	present, err := inspect(filepath.Join(dir, "missing"))
	if err != nil || present {
		t.Fatalf("present = %v, err = %v", present, err)
	}
	p := filepath.Join(dir, "hosts")
	if err := os.WriteFile(p, []byte("127.0.0.1 localhost\n"+line("care.local")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	present, err = inspect(p)
	if err != nil || !present {
		t.Fatalf("present = %v, err = %v", present, err)
	}
	present, err = inspect(dir)
	if err == nil || present {
		t.Fatalf("directory read became a definite answer: present = %v, err = %v", present, err)
	}
}

func TestWithoutHostOrMarker(t *testing.T) {
	const other = "127.0.0.1 old-clinic.local " + marker + "\n"
	const target = "10.0.0.5 care.local\n"
	const keep = "127.0.0.1 localhost\n"
	for _, c := range []struct {
		name, in, want string
		changed        bool
	}{
		{"target host without marker", keep + target, keep, true},
		{"other marked host", keep + other, keep, true},
		{"both", keep + target + other, keep, true},
		{"none", keep + "127.0.0.1 mycare.local # someone else\n",
			keep + "127.0.0.1 mycare.local # someone else\n", false},
		{"marked line naming the clinic counts once",
			keep + "127.0.0.1 care.local other.local " + marker + "\n", keep, true},
	} {
		got, changed := withoutHostOrMarker(c.in, "care.local")
		if got != c.want || changed != c.changed {
			t.Errorf("%s: got %q (%v), want %q (%v)", c.name, got, changed, c.want, c.changed)
		}
	}
}

func TestWithoutHost(t *testing.T) {
	cases := []struct {
		name, in, want string
		changed        bool
	}{
		{"drops a CARE line", "127.0.0.1 localhost\n127.0.0.1 care.local # care-clinic\n", "127.0.0.1 localhost\n", true},
		{"drops a line from any tool", "10.0.0.5\tCARE.local\n::1 localhost\n", "::1 localhost\n", true},
		{"keeps other names on a shared line", "127.0.0.1 care.local other.local # dev\n", "127.0.0.1 other.local # dev\n", true},
		{"keeps windows line endings", "127.0.0.1 localhost\r\n127.0.0.1 care.local\r\n", "127.0.0.1 localhost\r\n", true},
		{"ignores comments and similar names", "# 127.0.0.1 care.local\n127.0.0.1 mycare.local care.localhost\n", "# 127.0.0.1 care.local\n127.0.0.1 mycare.local care.localhost\n", false},
		{"ignores the IP column", "care.local localhost\n", "care.local localhost\n", false},
	}
	for _, c := range cases {
		got, changed := withoutHost(c.in, "care.local")
		if got != c.want || changed != c.changed {
			t.Errorf("%s: got %q (%v), want %q (%v)", c.name, got, changed, c.want, c.changed)
		}
	}
}
