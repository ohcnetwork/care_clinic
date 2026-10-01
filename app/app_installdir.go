package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ohcnetwork/care_desktop/app/internal/clinic"
)

func (a *App) installDir() string {
	base := filepath.Dir(a.configPath())
	if runtime.GOOS == "windows" {
		if home, err := os.UserHomeDir(); err == nil && filepath.IsAbs(home) {
			base = filepath.Join(home, appDirName)
		}
	}
	return filepath.Join(base, installSubdir)
}

const installSubdir = "install"

var installUserFiles = map[string]bool{"backend.env": true, "frontend.env": true}

const gitkeepPlaceholder = ".gitkeep"

func (a *App) ensureInstallDir() (string, error) {
	dest := a.installDir()
	err := fs.WalkDir(a.installFS, "install", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, "install")
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" || rel == gitkeepPlaceholder {
			return nil
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if installUserFiles[rel] {
			if _, err := os.Stat(target); err == nil {
				return nil
			}
		}
		data, err := fs.ReadFile(a.installFS, p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(rel, ".sh") {
			mode = 0o755
		}
		return os.WriteFile(target, data, mode)
	})
	if err != nil {
		return dest, err
	}
	return dest, os.MkdirAll(filepath.Join(dest, backupStateDir), 0o755)
}

const backupStateDir = "backup-state"

func (a *App) engine() *clinic.Clinic {
	cfg := a.loadConfig()
	return &clinic.Clinic{
		InstallDir: a.installDir(),
		MDNSName:   strings.TrimSuffix(strings.TrimSpace(cfg.MDNSName), ".local"),
		BackupDir:  cfg.BackupDir,
		Pins:       a.pins,
		Log:        a.logln,
		Confirm:    a.confirmDialog,
	}
}

func (a *App) engineForUpdate() *clinic.Clinic {
	e := a.engine()
	e.Abandon = func() bool { return !a.updatesAllowed() }
	return e
}
