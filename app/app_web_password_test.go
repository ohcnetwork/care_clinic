package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/clinic"
)

const fakeWebPasswordDocker = `#!/bin/sh
printf '%s\n' "$1 $2" >> "$CARE_FAKE_CALLS"
case "$1 $2" in
  'compose ps')
    if [ "$CARE_FAKE_STATE" = stopped ]; then printf 'backend exited\n'; else printf 'backend running\n'; fi
    exit 0 ;;
  'compose exec')
    printf '%s\n' "$CARE_WEB_PASSWORD" > "$CARE_FAKE_SECRET"
    printf 'care-clinic:password-set\n'
    exit 0 ;;
esac
exit 99
`

func webPasswordApp(t *testing.T) (*App, func() string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	a := settingsApp(t)
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(fakeWebPasswordDocker), 0o700); err != nil {
		t.Fatal(err)
	}
	calls, secret := filepath.Join(bin, "calls"), filepath.Join(bin, "secret")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CARE_FAKE_CALLS", calls)
	t.Setenv("CARE_FAKE_SECRET", secret)
	t.Setenv("CARE_FAKE_STATE", "")
	read := func() string {
		data, err := os.ReadFile(calls)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		return string(data)
	}
	return a, read, secret
}

func TestResetWebPasswordUsesTheUnlockedAdminPassword(t *testing.T) {
	a, calls, secret := webPasswordApp(t)
	if err := a.ResetWebPassword("admin", "NewWebSecret-918", settingsPassword); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(calls(), "compose exec") {
		t.Fatal("the password change never reached the backend")
	}
	if data, err := os.ReadFile(secret); err != nil || string(data) != "NewWebSecret-918\n" {
		t.Fatalf("the backend did not receive the new password: %q, %v", data, err)
	}
	if !a.VerifyAdminPassword(settingsPassword) {
		t.Fatal("resetting a web password changed the CARE Clinic admin password")
	}
}

func TestResetWebPasswordRejectsAWrongAdminPassword(t *testing.T) {
	a, calls, _ := webPasswordApp(t)
	err := a.ResetWebPassword("admin", "NewWebSecret-918", "WrongPassword123")
	if err == nil || !strings.Contains(err.Error(), "admin password does not match") {
		t.Fatalf("got %v", err)
	}
	if calls() != "" {
		t.Fatal("docker ran before the admin password was checked")
	}
}

func TestResetWebPasswordRefusesWhenCareCannotTakeIt(t *testing.T) {
	for _, state := range []string{"stopped", "plugin recovery", "not installed", "busy"} {
		t.Run(state, func(t *testing.T) {
			a, calls, _ := webPasswordApp(t)
			want := ""
			switch state {
			case "stopped":
				t.Setenv("CARE_FAKE_STATE", "stopped")
				want = clinic.ErrCareNotRunning.Error()
			case "plugin recovery":
				if err := os.WriteFile(filepath.Join(a.installDir(), "plugin-recovery.json"), []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
				want = "plugin recovery"
			case "not installed":
				a.cfg.SetupDone = false
				want = "admin password does not match"
			case "busy":
				a.jobMu.Lock()
				defer a.jobMu.Unlock()
				want = "something else is still running"
			}
			err := a.ResetWebPassword("admin", "NewWebSecret-918", settingsPassword)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("got %v, want %q", err, want)
			}
			if strings.Contains(calls(), "compose exec") {
				t.Fatal("the password change ran anyway")
			}
		})
	}
}
