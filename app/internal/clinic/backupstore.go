package clinic

import (
	"github.com/ohcnetwork/care_desktop/app/internal/backup"
)

const composeProject = "care-clinic"

func (e *Clinic) Backups() *backup.Store {
	s := &backup.Store{
		Dir:          e.InstallDir,
		BackupDir:    e.backupDir(),
		Image:        e.Pins.BackupImage,
		BackendImage: e.Pins.BackendImage,
		Host:         e.host(),
		Project:      composeProject,
		Log:          e.Log,
		Migrate:      e.migrateStaged,
	}
	s.EnsureImage = e.Builder().EnsureBackupImage
	s.EnsureRestoreImages = func() error {
		if err := e.Builder().EnsureBackendImage(); err != nil {
			return err
		}
		return e.Builder().EnsureBackupImage()
	}
	return backup.New(e.Runner(), s)
}

func (e *Clinic) BackupDirPath() string { return e.backupDir() }

func (e *Clinic) RestartBackupSidecar() error {
	if err := e.Backups().RecoverRestore(); err != nil {
		return err
	}
	return e.dc("up", "-d", "--force-recreate", "backup")
}
