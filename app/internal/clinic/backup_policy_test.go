package clinic

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupPolicyReadsSavedRetentionWithoutChangingSettings(t *testing.T) {
	t.Setenv("DB_BACKUP_RETENTION_PERIOD", "999")
	for _, tc := range []struct {
		name string
		env  string
		days int
	}{
		{"missing setting", "UNRELATED=preserve\n", 0},
		{"empty file", "", 0},
		{"empty setting", "DB_BACKUP_RETENTION_PERIOD=\n", 0},
		{"forever", "DB_BACKUP_RETENTION_PERIOD=0\n", 0},
		{"one day", "DB_BACKUP_RETENTION_PERIOD=1\n", 1},
		{"custom", "DB_BACKUP_RETENTION_PERIOD=37\n", 37},
		{"quoted", "export DB_BACKUP_RETENTION_PERIOD = ' 28 ' # custom policy\r\n", 28},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "backend.env")
			if err := os.WriteFile(path, []byte(tc.env), 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			e := &Clinic{InstallDir: dir}
			got, err := e.BackupPolicy()
			want := BackupPolicy{IntervalSeconds: 86400, RetentionDays: tc.days}
			if err != nil || got != want {
				t.Fatalf("BackupPolicy() = %+v, %v; want %+v", got, err, want)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != tc.env {
				t.Fatalf("policy read changed backend.env: %q, %v", data, err)
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("policy read rewrote backend.env")
			}
			if got := e.BackupKeepsForever(); got != (tc.days == 0) {
				t.Fatalf("legacy storage retention disagrees with policy: %v", got)
			}
		})
	}
}

func TestBackupPolicyRejectsInvalidRetention(t *testing.T) {
	for _, value := range []string{"-1", "-365", "1.5", "forever", "true", "14 days", "999999999999999999999999999999999999999"} {
		t.Run(value, func(t *testing.T) {
			dir := t.TempDir()
			body := "DB_BACKUP_RETENTION_PERIOD='" + value + "'\n"
			path := filepath.Join(dir, "backend.env")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := (&Clinic{InstallDir: dir}).BackupPolicy()
			if err == nil || !strings.Contains(err.Error(), "non-negative") || got != (BackupPolicy{}) {
				t.Fatalf("invalid retention %q became a usable policy: %+v, %v", value, got, err)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != body {
				t.Fatalf("invalid policy was rewritten: %q, %v", data, err)
			}
		})
	}
}

func TestBackupPolicyPropagatesBackendEnvironmentReadFailures(t *testing.T) {
	for _, kind := range []string{"missing", "directory", "malformed retention", "malformed unrelated setting"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "backend.env")
			switch kind {
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "malformed retention":
				if err := os.WriteFile(path, []byte("DB_BACKUP_RETENTION_PERIOD='unterminated\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "malformed unrelated setting":
				if err := os.WriteFile(path, []byte("DB_BACKUP_RETENTION_PERIOD=14\nUNRELATED='unterminated\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			e := &Clinic{InstallDir: dir}
			got, err := e.BackupPolicy()
			if err == nil || !strings.Contains(err.Error(), "couldn't read backup settings") || got != (BackupPolicy{}) {
				t.Fatalf("%s backend.env became a keep-forever policy: %+v, %v", kind, got, err)
			}
			if kind == "missing" && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("missing backend.env lost its underlying read error: %v", err)
			}
			access, secret := e.minioCreds()
			if access != "minioadmin" || secret != "minioadmin" || e.corazaMode() != "Off" {
				t.Fatal("readBackendEnv extraction changed legacy caller defaults on an unreadable environment")
			}
		})
	}
}

func TestBackupPolicyJSONUsesTheDesktopContract(t *testing.T) {
	data, err := json.Marshal(BackupPolicy{IntervalSeconds: 86400, RetentionDays: 37})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]int
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 || fields["interval_seconds"] != 86400 || fields["retention_days"] != 37 {
		t.Fatalf("backup policy bridge fields changed: %s", data)
	}
}
