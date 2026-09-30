package main

import (
	"errors"

	"github.com/ohcnetwork/care_desktop/app/internal/backup"
	"github.com/ohcnetwork/care_desktop/app/internal/clinic"
	"github.com/ohcnetwork/care_desktop/app/internal/residue"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) ScanResidue() (residue.Report, error) {
	if err := a.requireServer(); err != nil {
		return residue.Report{}, err
	}
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

func (a *App) PurgeResidue() error {
	return a.withServerJob(func() error {
		cfg := a.loadConfig()
		if cfg.SetupDone && !cfg.Removing {
			return errors.New("this computer already has a clinic set up - use Uninstall in the panel instead")
		}
		before, err := a.ScanResidue()
		if err != nil {
			return err
		}
		if before.Clean {
			return nil
		}
		if a.ctx == nil {
			return errors.New("open CARE Desktop to confirm removal of the earlier installation")
		}
		items := ""
		kept := "\n\nYour backups are kept. Keep the separate backup recovery file you saved during setup; it is needed to restore them."
		for _, t := range before.Traces {
			items += "\n  - " + t.Label + ": " + t.Detail
		}
		proceed, err := a.askToProceed("Remove the earlier CARE Desktop?",
			"This computer still has these from an earlier CARE Desktop:\n"+items+
				"\n\nClinic data, images, settings, installed files and old logs will be deleted. This cannot be undone."+kept,
			"Remove everything")
		if err != nil {
			return err
		}
		if !proceed {
			return nil
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
		after, err := a.ScanResidue()
		if err != nil {
			return err
		}
		a.reportPurge(after)
		if !after.Clean {
			return errors.New("cleanup is incomplete; remove the reported leftovers before setting up another clinic")
		}
		return nil
	})
}

func (a *App) residueInstallDir(e *clinic.Clinic) (string, error) {
	return residue.InstallDirFrom(e.Runner(), e.Project(), e.InstallDir)
}

func (a *App) reportPurge(after residue.Report) {
	if after.Clean {
		_, _ = wruntime.MessageDialog(a.ctx, wruntime.MessageDialogOptions{
			Type:    wruntime.InfoDialog,
			Title:   "This computer is clean",
			Message: "Everything from the earlier CARE Desktop has been removed. You can go ahead and set up the clinic.",
			Buttons: []string{"Continue"},
		})
		return
	}

	left := ""
	for _, t := range after.Traces {
		left += "\n  • " + t.Label + " — " + t.Detail
	}
	_, _ = wruntime.MessageDialog(a.ctx, wruntime.MessageDialogOptions{
		Type:  wruntime.WarningDialog,
		Title: "Some things are still here",
		Message: "Most of the earlier CARE Desktop was removed, but not these:" + left +
			"\n\nThey usually need an administrator. Try again, or ask your IT support to remove them.",
		Buttons: []string{"OK"},
	})
}
