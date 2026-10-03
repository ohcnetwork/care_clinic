package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/backup"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/crypto/bcrypt"
)

type SetupRecoveryStatus struct {
	BackupSaved              bool   `json:"backup_saved"`
	BackupVerified           bool   `json:"backup_verified"`
	CodesSaved               bool   `json:"codes_saved"`
	BackupPath               string `json:"backup_path"`
	CodesPath                string `json:"codes_path"`
	BackupProblem            string `json:"backup_problem"`
	CodesProblem             string `json:"codes_problem"`
	BackupKeyStored          bool   `json:"backup_key_stored"`
	BackupKeyNeedsEnrollment bool   `json:"backup_key_needs_enrollment"`
}

func (a *App) GetSetupRecoveryStatus() (status SetupRecoveryStatus, err error) {
	defer a.logError(&err)
	if err := a.requireServer(); err != nil {
		return SetupRecoveryStatus{}, err
	}
	cfg := a.loadConfig()
	return setupRecoveryStatus(cfg), nil
}

func (a *App) GetAdminRecoveryCodeCount() (count int, err error) {
	defer a.logError(&err)
	if err := a.requireServer(); err != nil {
		return 0, err
	}
	return a.loadConfig().adminRecoveryCount(), nil
}

func setupRecoveryStatus(cfg Config) SetupRecoveryStatus {
	status := SetupRecoveryStatus{
		BackupSaved: cfg.BackupCertificate != "", BackupVerified: cfg.BackupRecoveryVerified,
		CodesSaved: cfg.adminRecoveryCount() == 6, BackupPath: cfg.BackupRecoveryPath,
		CodesPath:                cfg.AdminRecoveryPath,
		BackupKeyStored:          cfg.BackupKeyEncrypted != "" && !cfg.BackupKeyNeedsEnrollment,
		BackupKeyNeedsEnrollment: cfg.BackupKeyNeedsEnrollment,
	}
	if status.BackupSaved {
		data, err := backup.ReadRecoveryFile(cfg.BackupRecoveryPath)
		switch {
		case errors.Is(err, os.ErrNotExist) || cfg.BackupRecoveryPath == "":
			status.BackupProblem = "missing"
		case err != nil:
			status.BackupProblem = "unreadable"
		case backup.VerifyRecoveryFile([]byte(cfg.BackupCertificate), data) != nil:
			status.BackupProblem = "mismatch"
		}
		if status.BackupProblem != "" {
			status.BackupVerified = false
		}
	}
	if status.CodesSaved {
		status.CodesProblem = adminCodesProblem(cfg)
	}
	return status
}

func adminCodesProblem(cfg Config) (problem string) {
	if cfg.AdminRecoveryPath == "" {
		return "missing"
	}
	info, err := os.Lstat(cfg.AdminRecoveryPath)
	if os.IsNotExist(err) {
		return "missing"
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64*1024 {
		return "unreadable"
	}
	file, err := os.Open(cfg.AdminRecoveryPath)
	if err != nil {
		return "unreadable"
	}
	defer func() {
		if err := file.Close(); err != nil {
			problem = "unreadable"
		}
	}()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "unreadable"
	}
	data, err := io.ReadAll(io.LimitReader(file, 64*1024+1))
	if err != nil || len(data) > 64*1024 {
		return "unreadable"
	}
	found := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "[ ] ") {
			found[recoveryHash(strings.TrimSpace(strings.TrimPrefix(line, "[ ] ")))] = true
		}
	}
	expected := make(map[string]bool)
	for _, hash := range cfg.AdminRecoveryHashes {
		if hash == "" || expected[hash] || !found[hash] {
			return "mismatch"
		}
		expected[hash] = true
	}
	return ""
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

var unsafeRecoveryFilename = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func recoveryFilename(clinic, kind, extension string) (string, error) {
	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("could not create a unique recovery filename: %w", err)
	}
	clinic = strings.Trim(unsafeRecoveryFilename.ReplaceAllString(clinic, "-"), ".-")
	if clinic == "" {
		clinic = "clinic"
	}
	return fmt.Sprintf("CARE-%s-%s-%s-%x.%s", clinic, kind,
		time.Now().UTC().Format("20060102-150405.000000000Z"), nonce, extension), nil
}

func (a *App) SaveSetupBackupRecovery(backupDir string) (bool, error) {
	return a.saveSetupBackupRecovery(backupDir, false)
}

// Replacement is explicit and only allowed before installation; the password-
// protected local copy is enrolled once the setup password is supplied.
func (a *App) ReplaceSetupBackupRecovery(backupDir string) (bool, error) {
	return a.saveSetupBackupRecovery(backupDir, true)
}

func (a *App) saveSetupBackupRecovery(backupDir string, replace bool) (bool, error) {
	return a.saveSetupBackupRecoveryWithDialog(backupDir, replace, func(opts wruntime.SaveDialogOptions) (string, error) {
		return wruntime.SaveFileDialog(a.ctx, opts)
	})
}

func (a *App) saveSetupBackupRecoveryWithDialog(backupDir string, replace bool, choose func(wruntime.SaveDialogOptions) (string, error)) (bool, error) {
	saved := false
	err := a.withServerJob(func() error {
		if err := a.requireRecoverySetup(); err != nil {
			return err
		}
		cfg := a.loadConfig()
		if cfg.BackupCertificate != "" && !replace {
			return errors.New("the recovery file has already been saved; select it using Verify saved file")
		}
		directory, err := recoverySaveDirectory()
		if err != nil {
			return fmt.Errorf("couldn't locate your Desktop: %w", err)
		}
		filename, err := recoveryFilename(cfg.MDNSName, "backup-recovery", "pem")
		if err != nil {
			return err
		}
		path, err := choose(wruntime.SaveDialogOptions{
			DefaultDirectory: directory,
			Title:            "Save backup recovery file on a separate secure drive",
			DefaultFilename:  filename,
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
		cfg.BackupKeyEncrypted = ""
		cfg.BackupKeyNeedsEnrollment = false
		if err := a.saveConfig(cfg); err != nil {
			return fmt.Errorf("the exported file was not activated; save a new recovery file before installing: %w", err)
		}
		saved = true
		return nil
	})
	return saved, err
}

func (a *App) ChooseRecoveryFile() (chosen string, err error) {
	defer a.logError(&err)
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
	return a.verifySetupBackupRecovery(backupDir, a.ChooseRecoveryFile)
}

func (a *App) verifySetupBackupRecovery(backupDir string, choose func() (string, error)) (bool, error) {
	verified := false
	err := a.withServerJob(func() error {
		cfg := a.loadConfig()
		if cfg.SetupDone || cfg.Removing {
			return errors.New("this clinic is already installed")
		}
		path, err := choose()
		if err == nil && path == "" {
			return nil
		}
		// Cancellation preserves verification; a new attempt must earn it again.
		cfg.BackupRecoveryVerified = false
		if saveErr := a.saveConfig(cfg); saveErr != nil {
			return errors.Join(err, saveErr)
		}
		if err != nil {
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
		directory, err := recoverySaveDirectory()
		if err != nil {
			return fmt.Errorf("couldn't locate your Desktop: %w", err)
		}
		filename, err := recoveryFilename(cfg.MDNSName, "desktop-admin-codes", "txt")
		if err != nil {
			return err
		}
		path, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
			DefaultDirectory: directory,
			Title:            "Save six Desktop admin recovery codes",
			DefaultFilename:  filename,
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
	cfg.AdminRecoveryPath = path
	cfg.RecoveryFailures, cfg.RecoveryRetryAfter = 0, 0
	if err := a.saveConfig(cfg); err != nil {
		return fmt.Errorf("could not activate the new recovery codes; keep the previous sheet and retry: %w", err)
	}
	a.emit("admin-recovery-codes-changed", cfg.adminRecoveryCount())
	return nil
}

func (a *App) OpenSetupRecoveryCodes() error {
	return a.withReadJob(func() error {
		if err := a.requireRecoverySetup(); err != nil {
			return err
		}
		cfg := a.loadConfig()
		if cfg.adminRecoveryCount() != 6 || adminCodesProblem(cfg) != "" {
			return errors.New("the recovery codes file is unavailable; save a fresh set before continuing")
		}
		return openDocument(cfg.AdminRecoveryPath)
	})
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
		if cfg.BackupKeyEncrypted != "" && !cfg.BackupKeyNeedsEnrollment {
			key, err := decryptBackupKey(cfg, currentPassword)
			if err != nil {
				return err
			}
			defer clear(key)
			cfg.BackupKeyEncrypted, err = encryptBackupKey(cfg, newPassword, key)
			if err != nil {
				return err
			}
		}
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
		// A recovery code authenticates a reset but cannot decrypt the old
		// password's key. Preserve ciphertext; require explicit PEM enrollment.
		cfg.BackupKeyNeedsEnrollment = true
		cfg.RecoveryFailures, cfg.RecoveryRetryAfter = 0, 0
		// One atomic config write both consumes the code and changes the password.
		if err := a.saveConfig(cfg); err != nil {
			return err
		}
		a.emit("admin-recovery-codes-changed", cfg.adminRecoveryCount())
		return nil
	})
}
