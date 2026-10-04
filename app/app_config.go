package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/atomicfile"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/mdns"
)

const appDirName = "care-clinic"

type Config struct {
	Role                     string    `json:"role"`
	ClientURL                string    `json:"client_url"`
	ClientCertificate        string    `json:"client_certificate,omitempty"`
	ClientCertificateOwned   bool      `json:"client_certificate_owned,omitempty"`
	SetupDone                bool      `json:"setup_done"`
	Removing                 bool      `json:"removing,omitempty"`
	MDNSName                 string    `json:"mdns_name"`
	BackupDir                string    `json:"backup_dir"`
	AdminPwHash              string    `json:"admin_pw_hash"`
	AdminRecoveryHashes      [6]string `json:"admin_recovery_hashes"`
	AdminRecoveryPath        string    `json:"admin_recovery_path,omitempty"`
	RecoveryFailures         int       `json:"recovery_failures,omitempty"`
	RecoveryRetryAfter       int64     `json:"recovery_retry_after,omitempty"`
	BackupCertificate        string    `json:"backup_certificate,omitempty"`
	BackupRecoveryPath       string    `json:"backup_recovery_path,omitempty"`
	BackupRecoveryVerified   bool      `json:"backup_recovery_verified,omitempty"`
	BackupKeyEncrypted       string    `json:"backup_key_encrypted,omitempty"`
	BackupKeyNeedsEnrollment bool      `json:"backup_key_needs_enrollment,omitempty"`
}

const (
	roleServer = "server"
	roleClient = "client"
)

func (cfg Config) hasServerSettings() bool {
	return cfg.SetupDone || cfg.Removing || cfg.AdminPwHash != "" || cfg.BackupDir != "" || cfg.MDNSName != "" ||
		cfg.BackupCertificate != "" || cfg.BackupKeyEncrypted != "" || cfg.BackupKeyNeedsEnrollment || cfg.adminRecoveryCount() > 0
}

func (cfg Config) adminRecoveryCount() int {
	count := 0
	for _, hash := range cfg.AdminRecoveryHashes {
		if hash != "" {
			count++
		}
	}
	return count
}

func normalizeRole(cfg Config) (Config, error) {
	if cfg.Role != "" && cfg.Role != roleServer && cfg.Role != roleClient {
		return Config{}, fmt.Errorf("the saved computer role is invalid: %q", cfg.Role)
	}
	if cfg.hasServerSettings() {
		if cfg.Role == roleClient {
			return Config{}, fmt.Errorf("client settings conflict with an existing server installation; recover the saved settings before continuing")
		}
		cfg.Role = roleServer
	}
	if cfg.Role == "" && (cfg.ClientURL != "" || cfg.ClientCertificate != "" || cfg.ClientCertificateOwned) {
		cfg.Role = roleClient
	}
	return cfg, nil
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir, err = os.UserHomeDir()
	}
	if err != nil {
		return "", fmt.Errorf("could not find the settings directory: %w", err)
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("the settings directory must be an absolute path: %q", dir)
	}
	return filepath.Join(dir, appDirName, "config.json"), nil
}

func loadConfig(path string) (Config, error) {
	var cfg *Config
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("could not read saved settings at %s: %w", path, err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("saved settings at %s are damaged; preserve this file and recover the settings before continuing: %w", path, err)
	}
	if cfg == nil {
		return Config{}, fmt.Errorf("saved settings at %s must be a JSON object", path)
	}
	if cfg.BackupDir != "" && !filepath.IsAbs(cfg.BackupDir) {
		return Config{}, fmt.Errorf("the saved backup directory must be an absolute path: %s", cfg.BackupDir)
	}
	if cfg.MDNSName != "" {
		if err := mdns.ValidateLabel(cfg.MDNSName); err != nil {
			return Config{}, fmt.Errorf("the saved clinic address is invalid: %w", err)
		}
	}
	return normalizeRole(*cfg)
}

func (a *App) configPath() string { return a.configFile }

func (a *App) loadConfig() Config {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	cfg, err := normalizeRole(a.cfg)
	if err != nil {
		return a.cfg
	}
	return cfg
}

func (a *App) saveConfig(cfg Config) error {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	current, err := normalizeRole(a.cfg)
	if err != nil {
		return err
	}
	cfg, err = normalizeRole(cfg)
	if err != nil {
		return err
	}
	if current.Role != "" && cfg.Role != current.Role {
		return fmt.Errorf("this computer's %s role cannot be changed", current.Role)
	}
	return a.writeConfigLocked(cfg)
}

func (a *App) writeConfigLocked(cfg Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.WritePrivate(a.configPath(), b); err != nil {
		return fmt.Errorf("could not save settings: %w", err)
	}
	a.cfg = cfg
	return nil
}

func (a *App) forgetConfig() error {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	cfg, err := normalizeRole(a.cfg)
	if err != nil {
		return err
	}
	if cfg.Role == roleClient {
		return a.writeConfigLocked(cfg)
	}
	return a.writeConfigLocked(Config{Role: cfg.Role})
}

func (a *App) resetConfigAfterUninstall() error {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	return a.writeConfigLocked(Config{})
}

// Existing files also identify a partial server setup when its settings are missing.
func (a *App) inferInstalledRole() error {
	cfg := a.loadConfig()
	if cfg.Role != "" {
		return nil
	}
	installed, err := a.installDirInUse()
	if err != nil || !installed {
		return err
	}
	cfg.Role = roleServer
	return a.saveConfig(cfg)
}

func (a *App) installDirInUse() (bool, error) {
	// Retained backups are recovery data, not an active server installation.
	dir := a.installDir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("could not check the earlier installation at %s: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.Name() != ".gitkeep" && entry.Name() != ".DS_Store" {
			return true, nil
		}
	}
	return false, nil
}

func (a *App) ClearRole() (err error) {
	return a.withJob(func() error {
		cfg := a.loadConfig()
		if cfg.Role == "" {
			return nil
		}
		set, err := a.setUp()
		if err != nil {
			return err
		}
		if set {
			return fmt.Errorf("this computer is already set up as a %s; uninstall its current setup first", cfg.Role)
		}
		if err := a.resetConfigAfterUninstall(); err != nil {
			return err
		}
		return a.restartAdvertise()
	})
}

// BeginServerSetup records the server role when the operator actually enters
// setup. The first screen only asks a question, so it writes nothing: a
// computer that is only being looked at keeps an empty settings file.
func (a *App) BeginServerSetup() (err error) {
	return a.selectRole(roleServer)
}

// SelectRole is the older entry point and stays for compatibility with an
// interface that still announces the choice up front.
func (a *App) SelectRole(role string) (err error) {
	if role != roleServer && role != roleClient {
		return fmt.Errorf("choose server or client")
	}
	return a.selectRole(role)
}

func (a *App) selectRole(role string) error {
	return a.withJob(func() error {
		if err := a.inferInstalledRole(); err != nil {
			return err
		}
		cfg := a.loadConfig()
		if cfg.Role != "" && cfg.Role != role {
			if role == roleClient {
				return errors.New(unfinishedServerSetup)
			}
			return fmt.Errorf("this computer is already a %s; its role cannot be changed", cfg.Role)
		}
		cfg.Role = role
		if err := a.saveConfig(cfg); err != nil {
			return err
		}
		return a.restartAdvertise()
	})
}

func (a *App) requireServer() error {
	if a.loadConfig().Role != roleServer {
		return fmt.Errorf("this operation is only available on a computer selected as the clinic server")
	}
	return nil
}
