package clinic

import (
	"os"

	"github.com/ohcnetwork/care_desktop/app/internal/backup"
	"github.com/ohcnetwork/care_desktop/app/internal/compose"
)

func (e *Clinic) Setup() error {
	if err := backup.CheckLocation(e.backupDir(), e.InstallDir); err != nil {
		return err
	}
	if err := e.genSecret(); err != nil {
		return err
	}
	if err := e.ApplyDomain(); err != nil {
		return err
	}
	if err := os.MkdirAll(e.backupDir(), 0o755); err != nil {
		return err
	}
	e.logln("Backups will go to: " + e.backupDir())
	if err := e.Backups().EnsureKeysDir(); err != nil {
		return err
	}
	e.logln("Building CARE's images - the backend and app build in the background while setup continues...")
	b := e.Builder()
	app := b.Start(compose.EnsureBackend, compose.EnsureFrontend)
	if err := e.setupWhileBuilding(b); err != nil {
		app.Cancel()
		return err
	}
	e.logln("Waiting for the backend and app images to finish building...")
	if err := app.Wait(); err != nil {
		return err
	}
	e.logln("Setup done.")
	return nil
}

func (e *Clinic) setupWhileBuilding(b *compose.Builder) error {
	if err := b.Parallel(compose.EnsureCaddy, compose.EnsureBackup); err != nil {
		return err
	}
	e.setUpThisComputerEarly()
	return e.Backups().GenBackupKeypair(e.BackupPassword)
}
