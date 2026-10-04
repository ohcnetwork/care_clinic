package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/release"
	"golang.org/x/crypto/bcrypt"
)

const settingsPassword = "ClinicPassword123"

func settingsApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	hash, err := bcrypt.GenerateFromPassword([]byte(settingsPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{
		configFile: filepath.Join(root, "care-clinic", "config.json"),
		cfg:        Config{SetupDone: true, AdminPwHash: string(hash)},
		pins:       &release.Pins{},
	}
	if err := os.MkdirAll(a.installDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"docker-compose.yml": "name: care-clinic\n",
		"backend.env":        "DB_BACKUP_RETENTION_PERIOD=14\nADDITIONAL_PLUGS=[]\n",
		"frontend.env":       "REACT_CARE_API_URL=https://care.local\n",
	} {
		if err := os.WriteFile(filepath.Join(a.installDir(), name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func TestSettingsReadsCanRunConcurrently(t *testing.T) {
	a := settingsApp(t)
	a.jobMu.RLock()
	defer a.jobMu.RUnlock()

	reads := []func() error{
		func() error {
			text, err := a.ReadEnv("backend", settingsPassword)
			if err != nil {
				return err
			}
			if text != "DB_BACKUP_RETENTION_PERIOD=14\nADDITIONAL_PLUGS=[]\n" {
				return fmt.Errorf("backend settings were not loaded from their file")
			}
			return nil
		},
		func() error {
			text, err := a.ReadEnv("frontend", settingsPassword)
			if err != nil {
				return err
			}
			if text != "REACT_CARE_API_URL=https://care.local\n" {
				return fmt.Errorf("frontend settings were not loaded from their file")
			}
			return nil
		},
		func() error {
			list, err := a.ReadPlugins()
			if err != nil {
				return err
			}
			if list == nil || len(list) != 0 {
				return fmt.Errorf("plugins were not loaded from backend.env")
			}
			return nil
		},
	}
	results := make(chan error, len(reads))
	for _, read := range reads {
		go func() { results <- read() }()
	}
	for range reads {
		if err := <-results; err != nil {
			t.Errorf("concurrent settings read failed: %v", err)
		}
	}
	if err := a.WriteEnv("backend", "DB_BACKUP_RETENTION_PERIOD=0\n", settingsPassword); err == nil {
		t.Fatal("a write bypassed the active readers")
	}
}

func TestSettingsReadsRespectMutationsAndClosing(t *testing.T) {
	a := settingsApp(t)
	a.jobMu.Lock()
	text, envErr := a.ReadEnv("backend", settingsPassword)
	list, pluginErr := a.ReadPlugins()
	a.jobMu.Unlock()
	for _, err := range []error{envErr, pluginErr} {
		if err == nil || !strings.Contains(err.Error(), "still running") {
			t.Fatalf("a read bypassed a mutation: %v", err)
		}
	}
	if text != "" || list != nil {
		t.Fatal("a rejected read returned settings")
	}
	if _, err := a.ReadEnv("backend", settingsPassword); err != nil {
		t.Fatalf("finished work left settings blocked: %v", err)
	}
	a.jobMu.Lock()
	a.closing = true
	a.jobMu.Unlock()
	if _, err := a.ReadEnv("backend", settingsPassword); err == nil || !strings.Contains(err.Error(), "closing") {
		t.Fatalf("settings were read while closing: %v", err)
	}
}

func TestSettingsReadsKeepAuthorizationAndLifecycleGuards(t *testing.T) {
	for _, state := range []string{"wrong password", "not installed", "removing"} {
		t.Run(state, func(t *testing.T) {
			a := settingsApp(t)
			password := settingsPassword
			switch state {
			case "wrong password":
				password = "wrong"
			case "not installed":
				a.cfg.SetupDone = false
			case "removing":
				a.cfg.Removing = true
			}
			if text, err := a.ReadEnv("backend", password); err == nil || text != "" {
				t.Fatalf("settings read bypassed its guard: %v", err)
			}
			if state != "wrong password" {
				if list, err := a.ReadPlugins(); err == nil || list != nil {
					t.Fatalf("plugin read bypassed its guard: %v", err)
				}
			}
		})
	}
}

func TestRetentionReadUsesSavedValue(t *testing.T) {
	a := settingsApp(t)
	for _, days := range []string{"14", "37", "0"} {
		want := "DB_BACKUP_RETENTION_PERIOD=" + days + "\n"
		if err := a.WriteEnv("backend", want, settingsPassword); err != nil {
			t.Fatal(err)
		}
		got, err := a.ReadEnv("backend", settingsPassword)
		if err != nil || got != want {
			t.Fatalf("saved retention was not loaded: %q, %v", got, err)
		}
	}
}
