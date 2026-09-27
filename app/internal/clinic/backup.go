package clinic

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/storage"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/diskspace"
)

func (e *Clinic) BackupNow() error {
	if !e.Backups().BackupEncryptionOn() {
		return errors.New("backup encryption is not set up on this install, so a backup " +
			"would be written unencrypted - run setup again and set a backup password")
	}
	need := e.manualBackupNeed()
	if err := e.backupRoomFor(need); err != nil {
		return err
	}
	ts := time.Now().Format("20060102-150405")
	name := "care-manual-" + ts + ".dump.enc"
	if err := e.dc("exec", "-T", "backup", "sh", "/backup.sh", "once", "manual-"+ts); err != nil {
		if roomErr := e.backupRoomFor(need); roomErr != nil {
			return roomErr
		}
		return fmt.Errorf("could not write the backup - CARE must be running to take one (%w)", err)
	}
	e.logln("Backup written (database and uploaded files): " +
		filepath.Join(e.backupDir(), name) + " and " +
		filepath.Join(e.backupDir(), "files-manual-"+ts+".tar.gz.enc"))
	return nil
}

func (e *Clinic) manualBackupNeed() uint64 {
	daily, _, found, err := storage.LatestSets(e.backupDir())
	if err != nil {
		return storage.BackupFloor
	}
	return storage.BackupNeed(daily, found)
}

func (e *Clinic) backupRoomFor(need uint64) error {
	u, err := diskspace.Of(e.backupDir())
	if err != nil || u.Free >= need {
		return nil
	}
	return fmt.Errorf("not enough space in the backup folder: this backup needs about %s and only %s is free. "+
		"Free up space on that drive, or choose another backup folder on the Backups tab",
		storage.Human(need), storage.Human(u.Free))
}

func (e *Clinic) BackupKeepsForever() bool {
	v := strings.TrimSpace(e.backendEnv()["DB_BACKUP_RETENTION_PERIOD"])
	n, err := strconv.Atoi(v)
	return v == "" || (err == nil && n == 0)
}

func (e *Clinic) DockerDiskFree() (free, total uint64, err error) {
	out, err := e.Runner().CaptureIn(15*time.Second, nil, "docker", "compose", "exec", "-T", "backup", "df", "-Pk", "/")
	if err != nil {
		return 0, 0, err
	}
	return storage.ParseDF(out)
}

func (e *Clinic) DockerRootDir() (string, error) {
	out, err := e.Runner().CaptureIn(5*time.Second, nil, "docker", "info", "--format", "{{.DockerRootDir}}")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(out) {
		return "", fmt.Errorf("docker reported an unusable data directory %q", out)
	}
	return out, nil
}
