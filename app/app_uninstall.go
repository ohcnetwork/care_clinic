package main

import (
	"errors"
	"strings"

	"github.com/ohcnetwork/care_desktop/app/internal/clinic"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/autostart"
)

func (a *App) RunUninstall(removeImages, removeBackups, removeRancher bool, adminPassword string) error {
	return a.run(func() error {
		if err := a.requireAdmin(adminPassword); err != nil {
			return err
		}
		if _, err := a.ScanResidue(); err != nil {
			return err
		}
		e := a.engine()
		if !removeBackups {
			if err := e.Backups().PreserveBackupCertificate(); err != nil {
				return err
			}
		}
		if err := a.beginRemoval(); err != nil {
			return err
		}
		if err := e.Uninstall(clinic.UninstallOptions{
			RemoveImages:     removeImages,
			RemoveInstallDir: true,
			RemoveBackups:    removeBackups,
		}); err != nil {
			return err
		}
		if autostart.Enabled() {
			if err := autostart.Set(false); err != nil {
				return err
			}
		}
		if err := a.reportUninstall(removeImages); err != nil {
			return err
		}
		if err := a.resetConfigAfterUninstall(); err != nil {
			return err
		}
		if removeRancher {
			if err := a.provisioner().RemoveRancherDesktop(); err != nil {
				a.logln("CARE was removed, but Rancher Desktop could not be fully removed: " + err.Error())
			}
		}
		a.logln("Uninstall complete.")
		a.emit("uninstalled", true)
		return nil
	}, false, "uninstall")
}

func (a *App) reportUninstall(removeImages bool) error {
	after, err := a.ScanResidue()
	if err != nil {
		return err
	}
	var items []string
	for _, t := range after.Traces {
		if t.ID == "config" || t.ID == "secret" || (t.ID == "images" && !removeImages) {
			continue
		}
		items = append(items, t.Label+": "+t.Detail)
	}
	if len(items) > 0 {
		return errors.New("cleanup is incomplete; settings were kept so it can be retried:\n" + strings.Join(items, "\n"))
	}
	return nil
}
