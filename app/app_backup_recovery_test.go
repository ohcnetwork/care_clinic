package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/backup"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func TestRecoveryFilenamesPortableAndUnique(t *testing.T) {
	seen := make(map[string]bool)
	for _, kind := range []struct{ name, extension string }{
		{"backup-recovery", "pem"}, {"desktop-admin-codes", "txt"},
	} {
		pattern := regexp.MustCompile(`^CARE-clinic-name-` + kind.name + `-\d{8}-\d{6}\.\d{9}Z-[a-f0-9]{12}\.` + kind.extension + `$`)
		for i := 0; i < 100; i++ {
			name, err := recoveryFilename("../clinic:name\\", kind.name, kind.extension)
			if err != nil || !pattern.MatchString(name) || seen[name] {
				t.Fatalf("invalid or repeated filename %q: %v", name, err)
			}
			seen[name] = true
		}
	}
}

func TestSetupBackupReplacementRequiresNewVerification(t *testing.T) {
	a := settingsApp(t)
	cfg := a.loadConfig()
	cfg.SetupDone = false
	cfg.AdminPwHash = ""
	if err := a.saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	firstPath := filepath.Join(t.TempDir(), "first.pem")
	firstName := ""
	saved, err := a.saveSetupBackupRecoveryWithDialog("", false, func(options wruntime.SaveDialogOptions) (string, error) {
		firstName = options.DefaultFilename
		return firstPath, nil
	})
	if err != nil || !saved {
		t.Fatalf("first export: %v, %v", saved, err)
	}
	firstKey, err := backup.ReadRecoveryFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg = a.loadConfig()
	cfg.BackupRecoveryVerified = true
	if err := a.saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	before := a.loadConfig()
	for _, path := range []string{"", firstPath} {
		saved, err := a.saveSetupBackupRecoveryWithDialog("", true, func(wruntime.SaveDialogOptions) (string, error) {
			return path, nil
		})
		if saved || (err != nil) != (path != "") || before != a.loadConfig() {
			t.Fatal("cancelled/failed replacement changed the existing verified key")
		}
	}
	secondPath := filepath.Join(t.TempDir(), "second.pem")
	saved, err = a.saveSetupBackupRecoveryWithDialog("", true, func(options wruntime.SaveDialogOptions) (string, error) {
		if options.DefaultFilename == firstName {
			t.Fatal("replacement suggested the same filename")
		}
		return secondPath, nil
	})
	if err != nil || !saved {
		t.Fatalf("replacement: %v, %v", saved, err)
	}
	after := a.loadConfig()
	newKey, err := backup.ReadRecoveryFile(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if after.BackupRecoveryVerified || after.BackupRecoveryPath != secondPath ||
		backup.VerifyRecoveryFile([]byte(after.BackupCertificate), firstKey) == nil ||
		backup.VerifyRecoveryFile([]byte(after.BackupCertificate), newKey) != nil {
		t.Fatal("replacement did not invalidate previous verification and require the new key")
	}
	if setupRecoveryStatus(after).BackupVerified {
		t.Fatal("replacement key was marked verified without selecting it")
	}
}

func TestSetupBackupVerificationClearsFailedChecks(t *testing.T) {
	certificate, key, err := backup.GenerateRecoveryFile()
	if err != nil {
		t.Fatal(err)
	}
	_, oldKey, err := backup.GenerateRecoveryFile()
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"correct", "older key", "missing", "dialog error", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			a := settingsApp(t)
			source := filepath.Join(t.TempDir(), "current.pem")
			old := filepath.Join(t.TempDir(), "older.pem")
			for path, data := range map[string][]byte{source: key, old: oldKey} {
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cfg := a.loadConfig()
			cfg.SetupDone = false
			cfg.AdminPwHash = ""
			cfg.BackupCertificate = string(certificate)
			cfg.BackupRecoveryPath = source
			cfg.BackupRecoveryVerified = true
			if err := a.saveConfig(cfg); err != nil {
				t.Fatal(err)
			}
			verified, err := a.verifySetupBackupRecovery("", func() (string, error) {
				switch scenario {
				case "older key":
					return old, nil
				case "missing":
					return filepath.Join(t.TempDir(), "missing.pem"), nil
				case "dialog error":
					return "", errors.New("file picker failed")
				case "cancel":
					return "", nil
				default:
					return source, nil
				}
			})
			success := scenario == "correct"
			if verified != success || (err != nil) != (!success && scenario != "cancel") {
				t.Fatalf("verified=%v, err=%v", verified, err)
			}
			expected := cfg
			expected.BackupRecoveryVerified = success || scenario == "cancel"
			if after := a.loadConfig(); after != expected {
				t.Fatal("verification did not preserve the key identity and clear only a failed check")
			}
			status, err := a.GetSetupRecoveryStatus()
			if err != nil || status.BackupVerified != expected.BackupRecoveryVerified {
				t.Fatalf("reloaded verification=%v, err=%v", status.BackupVerified, err)
			}
		})
	}
}

func TestBackupRecoveryExport(t *testing.T) {
	certificate, key, err := backup.GenerateRecoveryFile()
	if err != nil {
		t.Fatal(err)
	}
	otherCertificate, otherKey, err := backup.GenerateRecoveryFile()
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{
		"saved", "alternate", "cancel", "dialog error", "wrong password", "missing", "mismatch",
		"installed mismatch", "installed missing", "existing target", "protected target", "busy", "client",
	} {
		t.Run(scenario, func(t *testing.T) {
			a := settingsApp(t)
			source := filepath.Join(t.TempDir(), "original.pem")
			if err := os.WriteFile(source, key, 0o600); err != nil {
				t.Fatal(err)
			}
			certPath := filepath.Join(a.installDir(), "keys", "backup-cert.pem")
			if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(certPath, certificate, 0o600); err != nil {
				t.Fatal(err)
			}
			cfg := a.loadConfig()
			cfg.BackupCertificate = string(certificate)
			cfg.BackupRecoveryPath = source
			cfg.BackupRecoveryVerified = true
			if err := a.saveConfig(cfg); err != nil {
				t.Fatal(err)
			}
			if scenario == "client" {
				a.cfg.Role = "client"
			}
			password, selected := settingsPassword, ""
			target := filepath.Join(t.TempDir(), "copy.pem")
			switch scenario {
			case "alternate":
				selected = source
				cfg.BackupRecoveryPath = filepath.Join(t.TempDir(), "lost.pem")
				if err := a.saveConfig(cfg); err != nil {
					t.Fatal(err)
				}
			case "wrong password":
				password = "wrong"
			case "missing":
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
			case "mismatch":
				if err := os.WriteFile(source, otherKey, 0o600); err != nil {
					t.Fatal(err)
				}
			case "installed mismatch":
				if err := os.WriteFile(certPath, otherCertificate, 0o600); err != nil {
					t.Fatal(err)
				}
			case "installed missing":
				if err := os.Remove(certPath); err != nil {
					t.Fatal(err)
				}
			case "existing target":
				if err := os.WriteFile(target, []byte("do not overwrite"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "protected target":
				target = filepath.Join(a.installDir(), "copy.pem")
			case "busy":
				a.jobMu.Lock()
				defer a.jobMu.Unlock()
			}
			before := a.loadConfig()
			dialogCalls := 0
			saved, err := a.exportBackupRecovery(password, selected, func(options wruntime.SaveDialogOptions) (string, error) {
				dialogCalls++
				if !strings.HasSuffix(options.DefaultFilename, ".pem") || !strings.Contains(options.DefaultFilename, "-backup-recovery-") {
					t.Fatalf("unexpected export filename %q", options.DefaultFilename)
				}
				if scenario == "cancel" {
					return "", nil
				}
				if scenario == "dialog error" {
					return "", errors.New("dialog unavailable")
				}
				return target, nil
			})
			success := scenario == "saved" || scenario == "alternate"
			if saved != success || (err != nil) != (!success && scenario != "cancel") {
				t.Fatalf("saved=%v, err=%v", saved, err)
			}
			after := a.loadConfig()
			if success {
				if after.BackupKeyEncrypted == "" {
					t.Fatal("legacy installation was not enrolled")
				}
				unlocked, err := decryptBackupKey(after, password)
				if err != nil || !bytes.Equal(unlocked, key) {
					t.Fatal("enrolled encrypted key cannot be unlocked")
				}
				clear(unlocked)
				after.BackupKeyEncrypted = before.BackupKeyEncrypted
			}
			if before != after {
				t.Fatal("export changed recovery identity or unrelated configuration")
			}
			switch scenario {
			case "wrong password", "missing", "mismatch", "installed mismatch", "installed missing", "busy", "client":
				if dialogCalls != 0 {
					t.Fatal("save dialog opened before authorization and key verification")
				}
			}
			data, readErr := os.ReadFile(target)
			if success {
				if readErr != nil || !bytes.Equal(data, key) || backup.VerifyRecoveryFile(certificate, data) != nil {
					t.Fatal("export did not preserve the existing compatible private key")
				}
				info, err := os.Stat(target)
				if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
					t.Fatal("export is not private")
				}
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
				reloaded, err := loadConfig(a.configPath())
				if err != nil {
					t.Fatal(err)
				}
				a.cfg = reloaded
				nextPath := filepath.Join(t.TempDir(), "password-only.pem")
				saved, err := a.exportBackupRecovery(password, "", func(wruntime.SaveDialogOptions) (string, error) {
					return nextPath, nil
				})
				next, readErr := os.ReadFile(nextPath)
				if !saved || err != nil || readErr != nil || !bytes.Equal(next, key) {
					t.Fatalf("password-only re-download after deleting source failed: %v", err)
				}
				if a.loadConfig() != reloaded {
					t.Fatal("password-only export modified the key or configuration")
				}
			} else if scenario == "existing target" {
				if readErr != nil || string(data) != "do not overwrite" {
					t.Fatal("existing file changed")
				}
			} else if !os.IsNotExist(readErr) {
				t.Fatal("failed or cancelled export created a file")
			}
		})
	}
}
