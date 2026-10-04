package clinic

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/release"
)

func TestTeardownInspectsBeforeDeleting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	root := t.TempDir()
	trace := filepath.Join(root, "calls")
	t.Setenv("CARE_COMMAND_TRACE", trace)
	t.Setenv("PATH", root)
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$CARE_COMMAND_TRACE"
case "$1 $2" in
  'ps -aq') printf 'container-id\n' ;;
  'volume ls') exit 1 ;;
  'network ls') printf 'network-id\n' ;;
  *) exit 99 ;;
esac
`
	if err := os.WriteFile(filepath.Join(root, "docker"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	e := &Clinic{InstallDir: root, BackupDir: filepath.Join(root, "backups"), Pins: &release.Pins{}}
	if err := e.TeardownProject(); err == nil {
		t.Fatal("teardown ignored a failed resource inspection")
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "rm ") {
		t.Fatalf("teardown deleted resources after an incomplete inspection: %s", data)
	}
}

func TestInstallRemovalRejectsUnownedDirectories(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "personal.txt")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (&Clinic{InstallDir: root}).removeInstallFiles(false); err == nil {
		t.Fatal("an unowned directory was accepted for removal")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "keep" {
		t.Fatal("unrelated files were changed")
	}
}

func TestInstallRemovalHandlesUnusedCertificate(t *testing.T) {
	for _, tc := range []struct {
		name             string
		removeUnusedKey  bool
		encryptedBackups bool
		wantKey          bool
	}{
		{"failed setup without backups", true, false, false},
		{"failed setup with backups", true, true, true},
		{"normal uninstall", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			e := &Clinic{
				InstallDir: filepath.Join(root, "care-clinic", "install"),
				BackupDir:  filepath.Join(root, "backups"),
				Pins:       &release.Pins{},
			}
			keys := filepath.Join(e.InstallDir, "keys")
			if err := os.MkdirAll(keys, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(keys, "backup-cert.pem"), []byte("public certificate"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := e.Backups().PreserveBackupCertificate(); err != nil {
				t.Fatal(err)
			}
			dump := filepath.Join(e.BackupDir, "care-20260101-010101.dump.enc")
			if tc.encryptedBackups {
				if err := os.WriteFile(dump, []byte("encrypted backup"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := e.removeInstallFiles(tc.removeUnusedKey); err != nil {
				t.Fatal(err)
			}
			key, err := os.ReadFile(filepath.Join(e.BackupDir, "backup-cert.pem"))
			if tc.wantKey {
				if err != nil || string(key) != "public certificate" {
					t.Fatalf("backup certificate was not retained: %q, %v", key, err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("unused certificate still blocks retry: %v", err)
			}
			if _, err := os.Stat(e.InstallDir); !os.IsNotExist(err) {
				t.Fatalf("installed files were not removed: %v", err)
			}
			foreign, err := e.Backups().ForeignRecoveryData()
			if err != nil || foreign != tc.wantKey {
				t.Fatalf("unexpected recovery state after removal: %v, %v", foreign, err)
			}
			if tc.encryptedBackups {
				if data, err := os.ReadFile(dump); err != nil || string(data) != "encrypted backup" {
					t.Fatalf("encrypted backup was changed: %q, %v", data, err)
				}
			}
		})
	}
}
