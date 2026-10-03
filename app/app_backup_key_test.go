package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/backup"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func TestEncryptedBackupKeyAuthentication(t *testing.T) {
	cert, key, err := backup.GenerateRecoveryFile()
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{MDNSName: "care.local", BackupCertificate: string(cert)}
	first, err := encryptBackupKey(cfg, settingsPassword, key)
	if err != nil {
		t.Fatal(err)
	}
	second, err := encryptBackupKey(cfg, settingsPassword, key)
	if err != nil || first == second {
		t.Fatal("encryption did not use fresh salt and nonce")
	}
	var firstEnvelope, secondEnvelope encryptedBackupKey
	if err := json.Unmarshal([]byte(first), &firstEnvelope); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(second), &secondEnvelope); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(firstEnvelope.Salt, secondEnvelope.Salt) || bytes.Equal(firstEnvelope.Nonce, secondEnvelope.Nonce) {
		t.Fatal("encryption reused its salt or nonce")
	}
	cfg.BackupKeyEncrypted = first
	got, err := decryptBackupKey(cfg, settingsPassword)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatal("encrypted private key did not round-trip")
	}
	clear(got)
	if strings.Contains(first, settingsPassword) || strings.Contains(first, "PRIVATE KEY") {
		t.Fatal("plaintext secret present in encrypted envelope")
	}
	for _, scenario := range []string{"password", "ciphertext", "nonce", "salt", "version", "clinic", "certificate", "reset", "oversized", "truncated"} {
		t.Run(scenario, func(t *testing.T) {
			current, password := cfg, settingsPassword
			var envelope encryptedBackupKey
			if err := json.Unmarshal([]byte(first), &envelope); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "password":
				password = "WrongPassword123"
			case "ciphertext":
				envelope.Ciphertext[0] ^= 1
			case "nonce":
				envelope.Nonce[0] ^= 1
			case "salt":
				envelope.Salt[0] ^= 1
			case "version":
				envelope.Version++
			case "clinic":
				current.MDNSName = "other.local"
			case "certificate":
				current.BackupCertificate += "\n"
			case "reset":
				current.BackupKeyNeedsEnrollment = true
			case "oversized":
				envelope.Ciphertext = make([]byte, 32769)
			case "truncated":
				envelope.Nonce = nil
			}
			encoded, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			current.BackupKeyEncrypted = string(encoded)
			got, err := decryptBackupKey(current, password)
			if err == nil || len(got) != 0 {
				t.Fatal("wrong password or tampering disclosed the key")
			}
		})
	}
}

func enrolledRecoveryApp(t *testing.T, certificate, key []byte) (*App, []string) {
	t.Helper()
	a, codes := recoveryApp(t)
	cfg := a.loadConfig()
	cfg.MDNSName = "care.local"
	cfg.BackupCertificate = string(certificate)
	cfg.BackupRecoveryPath = filepath.Join(t.TempDir(), "original.pem")
	cfg.BackupRecoveryVerified = true
	if err := os.WriteFile(cfg.BackupRecoveryPath, key, 0o600); err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(a.installDir(), "keys", "backup-cert.pem")
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, certificate, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareSetupBackupKey(&cfg, settingsPassword); err != nil {
		t.Fatal(err)
	}
	if err := a.saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(a.configPath())
	if err != nil || bytes.Contains(raw, key) || bytes.Contains(raw, []byte(settingsPassword)) || bytes.Contains(raw, []byte("RSA PRIVATE KEY")) {
		t.Fatal("configuration could not be read or contains plaintext secrets")
	}
	return a, codes
}

func TestEncryptedBackupKeyPasswordLifecycle(t *testing.T) {
	certificate, key, err := backup.GenerateRecoveryFile()
	if err != nil {
		t.Fatal(err)
	}
	const nextPassword = "NewDesktopPass123"
	t.Run("password change rewraps without original PEM", func(t *testing.T) {
		a, _ := enrolledRecoveryApp(t, certificate, key)
		before := a.loadConfig()
		if err := os.Remove(before.BackupRecoveryPath); err != nil {
			t.Fatal(err)
		}
		if err := a.ChangeAdminPassword(settingsPassword, nextPassword); err != nil {
			t.Fatal(err)
		}
		after := a.loadConfig()
		if after.BackupKeyEncrypted == before.BackupKeyEncrypted || after.BackupCertificate != before.BackupCertificate {
			t.Fatal("password change did not rewrap the same key")
		}
		got, err := decryptBackupKey(after, nextPassword)
		if err != nil || !bytes.Equal(got, key) {
			t.Fatal("new password cannot unlock unchanged key")
		}
		clear(got)
		if _, err := decryptBackupKey(after, settingsPassword); err == nil {
			t.Fatal("old password still unlocks new envelope")
		}
		path := filepath.Join(t.TempDir(), "redownload.pem")
		saved, err := a.exportBackupRecovery(nextPassword, "", func(wruntime.SaveDialogOptions) (string, error) { return path, nil })
		if err != nil || !saved {
			t.Fatal("password-only export after password change failed:", err)
		}
	})
	t.Run("failed password change is atomic", func(t *testing.T) {
		a, _ := enrolledRecoveryApp(t, certificate, key)
		before := a.loadConfig()
		a.configFile = t.TempDir()
		if err := a.ChangeAdminPassword(settingsPassword, nextPassword); err == nil || a.loadConfig() != before {
			t.Fatal("failed write changed password or encrypted key")
		}
	})
	t.Run("reset preserves ciphertext and explicitly requires enrollment", func(t *testing.T) {
		a, codes := enrolledRecoveryApp(t, certificate, key)
		before := a.loadConfig()
		if err := a.ResetAdminPassword(codes[0], nextPassword); err != nil {
			t.Fatal(err)
		}
		after := a.loadConfig()
		status := setupRecoveryStatus(after)
		if after.BackupKeyEncrypted != before.BackupKeyEncrypted || !status.BackupKeyNeedsEnrollment || status.BackupKeyStored {
			t.Fatal("reset destroyed ciphertext or hid the re-enrollment requirement")
		}
		if _, err := decryptBackupKey(after, nextPassword); err == nil {
			t.Fatal("reset reported usable password-only recovery before enrollment")
		}
		path := filepath.Join(t.TempDir(), "reenrolled.pem")
		saved, err := a.exportBackupRecovery(nextPassword, before.BackupRecoveryPath, func(wruntime.SaveDialogOptions) (string, error) { return path, nil })
		if err != nil || !saved || a.loadConfig().BackupKeyNeedsEnrollment {
			t.Fatal("matching PEM did not re-enroll reset password:", err)
		}
		got, err := decryptBackupKey(a.loadConfig(), nextPassword)
		if err != nil || !bytes.Equal(got, key) {
			t.Fatal("re-enrollment changed the key")
		}
		clear(got)
	})
	t.Run("reset with missing source does not silently rotate", func(t *testing.T) {
		a, codes := enrolledRecoveryApp(t, certificate, key)
		if err := os.Remove(a.loadConfig().BackupRecoveryPath); err != nil {
			t.Fatal(err)
		}
		if err := a.ResetAdminPassword(codes[0], nextPassword); err != nil {
			t.Fatal(err)
		}
		before := a.loadConfig()
		saved, err := a.exportBackupRecovery(nextPassword, "", func(wruntime.SaveDialogOptions) (string, error) {
			t.Fatal("save dialog opened without an available key")
			return "", nil
		})
		if saved || err == nil || !strings.Contains(err.Error(), "cannot be reconstructed") || a.loadConfig() != before {
			t.Fatal("reset lost or silently replaced the preserved key")
		}
	})
	t.Run("corrupt local key blocks change and requires explicit source", func(t *testing.T) {
		a, _ := enrolledRecoveryApp(t, certificate, key)
		cfg := a.loadConfig()
		cfg.BackupKeyEncrypted = "corrupt"
		if err := a.saveConfig(cfg); err != nil {
			t.Fatal(err)
		}
		if err := a.ChangeAdminPassword(settingsPassword, nextPassword); err == nil || a.loadConfig() != cfg {
			t.Fatal("corrupt key allowed password change")
		}
		if saved, err := a.exportBackupRecovery(settingsPassword, "", func(wruntime.SaveDialogOptions) (string, error) {
			t.Fatal("corrupt key silently fell back to the original PEM")
			return "", nil
		}); saved || err == nil {
			t.Fatal("corrupt key was accepted")
		}
		path := filepath.Join(t.TempDir(), "repaired.pem")
		if saved, err := a.exportBackupRecovery(settingsPassword, cfg.BackupRecoveryPath, func(wruntime.SaveDialogOptions) (string, error) { return path, nil }); !saved || err != nil {
			t.Fatal("explicit re-enrollment failed:", err)
		}
	})
	t.Run("failed enrollment preserves previous configuration and exported PEM", func(t *testing.T) {
		a, _ := enrolledRecoveryApp(t, certificate, key)
		cfg := a.loadConfig()
		cfg.BackupKeyEncrypted = ""
		if err := a.saveConfig(cfg); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(a.configPath()); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(a.configPath(), 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "keep-this.pem")
		saved, err := a.exportBackupRecovery(settingsPassword, cfg.BackupRecoveryPath, func(wruntime.SaveDialogOptions) (string, error) { return path, nil })
		if saved || err == nil || !strings.Contains(err.Error(), "PEM was exported") || a.loadConfig() != cfg {
			t.Fatalf("failed enrollment: saved=%v changed=%v error=%v", saved, a.loadConfig() != cfg, err)
		}
		got, readErr := backup.ReadRecoveryFile(path)
		if readErr != nil || !bytes.Equal(got, key) {
			t.Fatal("failed enrollment discarded the exported recovery file")
		}
		clear(got)
	})
}
