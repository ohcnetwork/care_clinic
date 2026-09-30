package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/backup"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/crypto/bcrypt"
)

type SetupRecoveryStatus struct {
	BackupSaved    bool `json:"backup_saved"`
	BackupVerified bool `json:"backup_verified"`
	CodesSaved     bool `json:"codes_saved"`
}

func (a *App) GetSetupRecoveryStatus() (SetupRecoveryStatus, error) {
	if err := a.requireServer(); err != nil {
		return SetupRecoveryStatus{}, err
	}
	cfg := a.loadConfig()
	return SetupRecoveryStatus{
		BackupSaved: cfg.BackupCertificate != "", BackupVerified: cfg.BackupRecoveryVerified,
		CodesSaved: cfg.adminRecoveryCount() == 6,
	}, nil
}

func (a *App) requireRecoverySetup() error {
	if err := a.requireServer(); err != nil {
		return err
	}
	cfg := a.loadConfig()
	if cfg.SetupDone || cfg.Removing || cfg.AdminPwHash != "" {
		return errors.New("recovery setup is locked after installation starts; retry installation with the recovery items already saved")
	}
	return nil
}

func (a *App) recoveryLocation(path, backupDir string) error {
	target := a.engine().BackupDirPath()
	if strings.TrimSpace(backupDir) != "" {
		target = filepath.Join(strings.TrimSpace(backupDir), "care-db-backups")
	}
	for _, protected := range []string{a.installDir(), filepath.Dir(a.configPath()), target, a.log.Folder()} {
		if protected != "" {
			if err := backup.CheckLocation(path, protected); err != nil {
				return fmt.Errorf("keep recovery materials outside CARE's settings, installation, logs and backup folder: %w", err)
			}
		}
	}
	return nil
}

// Never overwrite a user's file, even if the native save dialog approved it.
func saveRecoveryFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("could not save the recovery file; choose a new filename: %w", err)
	}
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return errors.Join(err, os.Remove(path))
	}
	return nil
}

func (a *App) SaveSetupBackupRecovery(backupDir string) (bool, error) {
	saved := false
	err := a.withServerJob(func() error {
		if err := a.requireRecoverySetup(); err != nil {
			return err
		}
		cfg := a.loadConfig()
		if cfg.BackupCertificate != "" {
			return errors.New("the recovery file has already been saved; select it using Verify saved file")
		}
		path, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
			Title:           "Save backup recovery file on a separate secure drive",
			DefaultFilename: "CARE-" + cfg.MDNSName + "-backup-recovery.pem",
		})
		if err != nil || path == "" {
			return err
		}
		if err := a.recoveryLocation(path, backupDir); err != nil {
			return err
		}
		cert, key, err := backup.GenerateRecoveryFile()
		if err != nil {
			return fmt.Errorf("could not generate the backup recovery file: %w", err)
		}
		if err := saveRecoveryFile(path, key); err != nil {
			return err
		}
		cfg.BackupCertificate = string(cert)
		cfg.BackupRecoveryPath = path
		cfg.BackupRecoveryVerified = false
		if err := a.saveConfig(cfg); err != nil {
			return fmt.Errorf("the exported file was not activated; save a new recovery file before installing: %w", err)
		}
		saved = true
		return nil
	})
	return saved, err
}

func (a *App) ChooseRecoveryFile() (string, error) {
	path, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title:   "Choose your backup recovery file",
		Filters: []wruntime.FileFilter{{DisplayName: "CARE backup recovery file", Pattern: "*.pem"}},
	})
	if err != nil || path == "" {
		return path, err
	}
	if _, err := backup.ReadRecoveryFile(path); err != nil {
		return "", err
	}
	return path, nil
}

func (a *App) VerifySetupBackupRecovery(backupDir string) (bool, error) {
	verified := false
	err := a.withServerJob(func() error {
		cfg := a.loadConfig()
		if cfg.SetupDone || cfg.Removing {
			return errors.New("this clinic is already installed")
		}
		path, err := a.ChooseRecoveryFile()
		if err != nil || path == "" {
			return err
		}
		if err := a.recoveryLocation(path, backupDir); err != nil {
			return err
		}
		data, err := backup.ReadRecoveryFile(path)
		if err != nil {
			return err
		}
		if err := backup.VerifyRecoveryFile([]byte(cfg.BackupCertificate), data); err != nil {
			return err
		}
		cfg.BackupRecoveryVerified = true
		cfg.BackupRecoveryPath = path
		if err := a.saveConfig(cfg); err != nil {
			return err
		}
		verified = true
		return nil
	})
	return verified, err
}

func recoveryHash(code string) string {
	normal := strings.ToUpper(strings.Join(strings.Fields(strings.ReplaceAll(code, "-", "")), ""))
	hash := sha256.Sum256([]byte(normal))
	return hex.EncodeToString(hash[:])
}

func generateAdminRecoveryCodes(clinic string) ([]byte, [6]string, error) {
	var text strings.Builder
	fmt.Fprintf(&text, "CARE Desktop admin recovery codes\nClinic: %s\nIssued: %s\n\n", clinic, time.Now().Format(time.RFC3339))
	text.WriteString("Keep this sheet secure, outside the clinic computer. You may print it.\nEach code works once. Mark a code used after resetting your password.\n\n")
	var hashes [6]string
	for i := 0; i < 6; i++ {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, hashes, err
		}
		raw := strings.ToUpper(hex.EncodeToString(random[:]))
		groups := make([]string, 0, 8)
		for j := 0; j < len(raw); j += 4 {
			groups = append(groups, raw[j:j+4])
		}
		code := strings.Join(groups, "-")
		fmt.Fprintf(&text, "[ ] %s\n", code)
		hashes[i] = recoveryHash(code)
	}
	text.WriteString("\nIn CARE Desktop, open Advanced > Forgot Desktop password?\nEnter one unused code and choose a new Desktop admin password.\nThis does NOT change your CARE web login or unlock backups.\nA replacement set invalidates every code on this sheet.\n")
	return []byte(text.String()), hashes, nil
}

func (a *App) SaveAdminRecoveryCodes(adminPassword, backupDir string) (bool, error) {
	saved := false
	err := a.withServerJob(func() error {
		cfg := a.loadConfig()
		if cfg.SetupDone {
			if err := a.requireAdmin(adminPassword); err != nil {
				return err
			}
		} else if err := a.requireRecoverySetup(); err != nil {
			return err
		}
		path, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
			Title:           "Save six Desktop admin recovery codes",
			DefaultFilename: "CARE-" + cfg.MDNSName + "-desktop-admin-codes.txt",
		})
		if err != nil || path == "" {
			return err
		}
		if err := a.recoveryLocation(path, backupDir); err != nil {
			return err
		}
		if err := a.saveAdminRecoveryCodes(path); err != nil {
			return err
		}
		saved = true
		return nil
	})
	return saved, err
}

func (a *App) saveAdminRecoveryCodes(path string) error {
	cfg := a.loadConfig()
	text, hashes, err := generateAdminRecoveryCodes(cfg.MDNSName)
	if err != nil {
		return err
	}
	if err := saveRecoveryFile(path, text); err != nil {
		return err
	}
	cfg.AdminRecoveryHashes = hashes
	cfg.RecoveryFailures, cfg.RecoveryRetryAfter = 0, 0
	if err := a.saveConfig(cfg); err != nil {
		return fmt.Errorf("could not activate the new recovery codes; keep the previous sheet and retry: %w", err)
	}
	return nil
}

func (a *App) ChangeAdminPassword(currentPassword, newPassword string) error {
	return a.withServerJob(func() error {
		if err := a.requireAdmin(currentPassword); err != nil {
			return err
		}
		if err := ValidatePassword(newPassword); err != nil {
			return err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		cfg := a.loadConfig()
		cfg.AdminPwHash = string(hash)
		return a.saveConfig(cfg)
	})
}

func (a *App) ResetAdminPassword(code, newPassword string) error {
	return a.withServerJob(func() error {
		cfg := a.loadConfig()
		if !cfg.SetupDone || cfg.Removing {
			return errors.New("Desktop password recovery is available only for an installed clinic")
		}
		now := time.Now().Unix()
		if cfg.RecoveryRetryAfter > now {
			return fmt.Errorf("too many recovery attempts; try again in %d seconds", cfg.RecoveryRetryAfter-now)
		}
		if err := ValidatePassword(newPassword); err != nil {
			return err
		}
		hash := recoveryHash(code)
		match := -1
		for i, candidate := range cfg.AdminRecoveryHashes {
			if subtle.ConstantTimeCompare([]byte(candidate), []byte(hash)) == 1 {
				match = i
			}
		}
		if match == -1 {
			if cfg.RecoveryFailures < 15 {
				cfg.RecoveryFailures++
			}
			if cfg.RecoveryFailures >= 5 {
				cfg.RecoveryRetryAfter = now + int64(cfg.RecoveryFailures-4)*60
			}
			if err := a.saveConfig(cfg); err != nil {
				return err
			}
			return errors.New("that recovery code is invalid or already used; use an unused code from the latest set")
		}
		passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		cfg.AdminRecoveryHashes[match] = ""
		cfg.AdminPwHash = string(passwordHash)
		cfg.RecoveryFailures, cfg.RecoveryRetryAfter = 0, 0
		// One atomic config write both consumes the code and changes the password.
		return a.saveConfig(cfg)
	})
}
