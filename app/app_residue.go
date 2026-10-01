package main

import (
	"errors"

	"github.com/ohcnetwork/care_desktop/app/internal/backup"
	"github.com/ohcnetwork/care_desktop/app/internal/clinic"
	"github.com/ohcnetwork/care_desktop/app/internal/residue"
)

// ScanResidue is available before a role is chosen and on a client, because
// both need to know what an earlier clinic left here. Only removal is
// restricted to the computer that hosts the clinic.
func (a *App) ScanResidue() (report residue.Report, err error) {
	defer a.logError(&err)
	return a.scanResidue()
}

func (a *App) scanResidue() (residue.Report, error) {
	e := a.engine()
	dir, err := a.residueInstallDir(e)
	if err != nil {
		return residue.Report{}, err
	}
	configPath := ""
	if cfg := a.loadConfig(); cfg.SetupDone || cfg.Removing || cfg.AdminPwHash != "" || cfg.BackupDir != "" {
		configPath = a.configPath()
	}
	return residue.Scan(residue.Options{
		Runner:     e.Runner(),
		Project:    e.Project(),
		InstallDir: dir,
		ConfigPath: configPath,
		Images:     e.Images(),
	})
}

func (a *App) keepChosenName(before Config) error {
	if before.SetupDone || before.Removing || before.MDNSName == "" {
		return nil
	}
	if err := a.saveConfig(Config{Role: before.Role, MDNSName: before.MDNSName}); err != nil {
		return err
	}
	return a.restartAdvertise()
}

// PurgeResidue removes what an earlier CARE Desktop left behind. The
// destructive confirmation belongs to the interface, which can show the traces
// in place; confirmed carries that answer. The returned report is the state
// afterwards, so the caller can say "clean" or "still here" without a second
// scan.
func (a *App) PurgeResidue(confirmed bool) (after residue.Report, err error) {
	err = a.withServerJob(func() error {
		cfg := a.loadConfig()
		if cfg.SetupDone && !cfg.Removing {
			return errors.New("this computer already has a clinic set up - use Uninstall in the panel instead")
		}
		before, err := a.scanResidue()
		if err != nil {
			return err
		}
		after = before
		if before.Clean {
			return nil
		}
		if !confirmed {
			return errors.New("confirm the removal of the earlier CARE Desktop before it can be removed")
		}
		e := a.engine()
		e.InstallDir, err = a.residueInstallDir(e)
		if err != nil {
			return err
		}
		if folder := a.log.Folder(); folder != "" {
			if err := backup.CheckLocation(e.BackupDirPath(), folder); err != nil {
				return err
			}
		}
		if err := e.Backups().PreserveBackupCertificate(); err != nil {
			return err
		}
		if err := a.beginRemoval(); err != nil {
			return err
		}
		a.logln("Removing the earlier CARE Desktop from this computer...")
		if err := e.Purge(); err != nil {
			return err
		}
		if err := a.reportUninstall(true); err != nil {
			return err
		}
		if err := a.log.PurgeFolder(); err != nil {
			return err
		}
		if err := a.forgetConfig(); err != nil {
			return err
		}
		if err := a.keepChosenName(cfg); err != nil {
			return err
		}
		after, err = a.scanResidue()
		if err != nil {
			return err
		}
		a.logPurge(after)
		if !after.Clean {
			return errors.New("cleanup is incomplete; remove the reported leftovers before setting up another clinic")
		}
		return nil
	})
	return after, err
}

func (a *App) residueInstallDir(e *clinic.Clinic) (string, error) {
	return residue.InstallDirFrom(e.Runner(), e.Project(), e.InstallDir)
}

func (a *App) logPurge(after residue.Report) {
	if after.Clean {
		a.logln("Everything from the earlier CARE Desktop has been removed.")
		return
	}
	for _, t := range after.Traces {
		a.logln("Still here after cleanup: " + t.Label + " - " + t.Detail)
	}
}
