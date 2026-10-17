package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func sameError(err, want error) bool {
	return err != nil && err.Error() == want.Error()
}

func TestUpdateChecksDefaultToEnabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"role":"server","setup_done":true,"admin_pw_hash":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.updateChecks(); !got.App || !got.Care {
		t.Fatalf("an existing install lost its update checks: %+v", got)
	}
	a := roleApp(t)
	if got := a.GetUpdateChecks(); !got.App || !got.Care {
		t.Fatalf("a fresh install starts with update checks off: %+v", got)
	}
}

func TestSetUpdateChecksRequiresTheAdminPassword(t *testing.T) {
	a := settingsApp(t)
	if err := a.SetUpdateChecks(false, false, "wrong password"); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("wrong password was accepted: %v", err)
	}
	if got := a.GetUpdateChecks(); !got.App || !got.Care {
		t.Fatalf("a rejected change still turned checks off: %+v", got)
	}
	if err := a.SetUpdateChecks(false, true, settingsPassword); err != nil {
		t.Fatal(err)
	}
	if got := a.GetUpdateChecks(); got.App || !got.Care {
		t.Fatalf("app checks were not turned off alone: %+v", got)
	}
	saved, err := loadConfig(a.configPath())
	if err != nil {
		t.Fatal(err)
	}
	if !saved.AppUpdateChecksOff || saved.CareUpdateChecksOff {
		t.Fatalf("the choice was not persisted: %+v", saved.updateChecks())
	}
	if err := a.SetUpdateChecks(true, false, settingsPassword); err != nil {
		t.Fatal(err)
	}
	if got := a.GetUpdateChecks(); !got.App || got.Care {
		t.Fatalf("CARE checks were not turned off alone: %+v", got)
	}
}

func TestSetUpdateChecksHonorsTheJobGuard(t *testing.T) {
	a := settingsApp(t)
	a.jobMu.Lock()
	err := a.SetUpdateChecks(false, false, settingsPassword)
	a.jobMu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("setting changed during a running job: %v", err)
	}
}

func TestAppUpdateChecksOffNeverContactsGitHub(t *testing.T) {
	a := settingsApp(t)
	a.pins.AppVersion = "1.2.3"
	a.cfg.AppUpdateChecksOff = true
	var requests atomic.Int32
	original := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: updateTransport(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, fmt.Errorf("unexpected update request: %s", r.URL)
	})}
	t.Cleanup(func() { http.DefaultClient = original })

	if _, err := a.CheckAppUpdate(); !sameError(err, errAppUpdateChecksOff) {
		t.Fatalf("check ran while app update checks were off: %v", err)
	}
	if err := a.InstallAppUpdate(); !sameError(err, errAppUpdateChecksOff) {
		t.Fatalf("install ran while app update checks were off: %v", err)
	}
	if !a.jobMu.TryLock() {
		t.Fatal("a refused update left the job lock held")
	}
	a.jobMu.Unlock()
	if requests.Load() != 0 {
		t.Fatalf("GitHub was contacted %d times", requests.Load())
	}
}

func TestCareUpdateChecksOffNeverResolvesTheCareRepositories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	a := settingsApp(t)
	bin := t.TempDir()
	marker := filepath.Join(bin, "git-called")
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	called := func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}

	a.cfg.CareUpdateChecksOff = true
	if err := a.CheckCareUpdate(); !sameError(err, errCareUpdateChecksOff) {
		t.Fatalf("check now was accepted while CARE update checks were off: %v", err)
	}
	if checking.Load() {
		t.Fatal("a refused check reserved the update-check flag")
	}
	if a.careCheckDue() {
		t.Fatal("the hourly watcher would still check")
	}
	a.checkCareUpdate()
	if err := a.ClinicAction("update", ""); !sameError(err, errCareUpdateChecksOff) {
		t.Fatalf("a staged CARE update was applied while checks were off: %v", err)
	}
	if !a.engine().HoldUpdates {
		t.Fatal("start would still apply a staged CARE update")
	}
	if called() {
		t.Fatal("a CARE repository was contacted while update checks were off")
	}

	a.cfg.CareUpdateChecksOff = false
	a.checkCareUpdate()
	if !called() {
		t.Fatal("the fixture did not observe an enabled check, so the disabled case proves nothing")
	}
}
