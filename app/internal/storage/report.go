package storage

import (
	"fmt"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/diskspace"
)

type Drive struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Path      string `json:"path"`
	Free      uint64 `json:"free"`
	Total     uint64 `json:"total"`
	Level     Level  `json:"level"`
	Message   string `json:"message"`
	Cleanable bool   `json:"cleanable"`
}

type BackupSpace struct {
	Dir               string `json:"dir"`
	Free              uint64 `json:"free"`
	Total             uint64 `json:"total"`
	Need              uint64 `json:"need"`
	SetBytes          uint64 `json:"set_bytes"`
	DaysLeft          int    `json:"days_left"`
	SharesDockerDrive bool   `json:"shares_docker_drive"`
	Level             Level  `json:"level"`
	Message           string `json:"message"`
}

type Report struct {
	CheckedAt      int64       `json:"checked_at"`
	Level          Level       `json:"level"`
	Headline       string      `json:"headline"`
	Drives         []Drive     `json:"drives"`
	Backup         BackupSpace `json:"backup"`
	LastRun        BackupRun   `json:"last_run"`
	NewestBackupAt int64       `json:"newest_backup_at"`
	Stale          bool        `json:"stale"`
}

type InstallResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	How     string `json:"how"`
	Free    uint64 `json:"free"`
	Need    uint64 `json:"need"`
}

func CheckInstall(install, docker diskspace.Usage) InstallResult {
	if docker.Free < InstallMinFree {
		return InstallResult{
			Message: fmt.Sprintf("%s free, needs %s", Human(docker.Free), Human(InstallMinFree)),
			How: fmt.Sprintf("Only %s is free on the drive the clinic's data will live on (%s). "+
				"CARE needs at least %s to install. Empty the Recycle Bin or Trash, remove large "+
				"downloads or unused programs, then check again.",
				Human(docker.Free), docker.Path, Human(InstallMinFree)),
			Free: docker.Free,
			Need: InstallMinFree,
		}
	}
	if install.Volume != docker.Volume && install.Free < InstallDirMinFree {
		return InstallResult{
			Message: fmt.Sprintf("%s free, needs %s", Human(install.Free), Human(InstallDirMinFree)),
			How: fmt.Sprintf("Only %s is free on the drive that holds CARE's settings (%s). "+
				"Free up at least %s there, then check again.",
				Human(install.Free), install.Path, Human(InstallDirMinFree)),
			Free: install.Free,
			Need: InstallDirMinFree,
		}
	}
	return InstallResult{OK: true, Message: Human(docker.Free) + " free", Free: docker.Free, Need: InstallMinFree}
}

func CheckRunning(docker diskspace.Usage) InstallResult {
	r := InstallResult{Free: docker.Free, Need: RunningCriticalFree, Message: Human(docker.Free) + " free"}
	if RunningLevel(docker.Free) != LevelCritical {
		r.OK = true
		return r
	}
	r.How = fmt.Sprintf("Only %s is left on the drive the clinic's data lives on (%s). "+
		"The database stops accepting changes when it runs out. Free up space, then start the clinic again.",
		Human(docker.Free), docker.Path)
	return r
}

func AssessDrive(id, label string, u diskspace.Usage) Drive {
	d := Drive{ID: id, Label: label, Path: u.Path, Free: u.Free, Total: u.Total, Level: RunningLevel(u.Free)}
	switch d.Level {
	case LevelCritical:
		d.Message = fmt.Sprintf("Only %s left. The clinic stops saving data when this runs out - free up space now.", Human(u.Free))
	case LevelLow:
		d.Message = fmt.Sprintf("%s left. Free up space soon.", Human(u.Free))
	default:
		d.Message = "Plenty of room for the clinic's data."
	}
	return d
}

func AssessBackup(dir string, u diskspace.Usage, set BackupSet, found, keepForever, sharesDocker bool) BackupSpace {
	need := BackupNeed(set, found)
	b := BackupSpace{
		Dir:               dir,
		Free:              u.Free,
		Total:             u.Total,
		Need:              need,
		SetBytes:          set.Bytes(),
		DaysLeft:          -1,
		SharesDockerDrive: sharesDocker,
		Level:             LevelOK,
	}
	if keepForever && found && !set.Manual {
		b.DaysLeft = DaysLeft(u.Free, need, set.Bytes())
	}
	switch {
	case u.Free < need:
		b.Level = LevelCritical
		b.Message = fmt.Sprintf("Not enough room for the next backup. It needs about %s and only %s is free.",
			Human(need), Human(u.Free))
	case u.Free < 2*need:
		b.Level = LevelLow
		b.Message = fmt.Sprintf("Room for only one more backup. Each one needs about %s; %s is free.",
			Human(need), Human(u.Free))
	case b.DaysLeft >= 0 && b.DaysLeft < 30:
		b.Level = LevelLow
		b.Message = fmt.Sprintf("Backups are kept forever, so this drive fills up in about %d days.", b.DaysLeft)
	case b.DaysLeft >= 0:
		b.Message = fmt.Sprintf("Each backup needs about %s. Room for about %d days of backups.", Human(need), b.DaysLeft)
	default:
		b.Message = fmt.Sprintf("Each backup needs about %s.", Human(need))
	}
	return b
}

func Summarise(r *Report, running bool, now time.Time) {
	levels := []Level{r.Backup.Level}
	for _, d := range r.Drives {
		levels = append(levels, d.Level)
	}
	inProgress := r.LastRun.State == "running" && now.Sub(time.Unix(r.LastRun.At, 0)) < 6*time.Hour
	r.Stale = running && r.NewestBackupAt > 0 && !inProgress &&
		now.Sub(time.Unix(r.NewestBackupAt, 0)) > StaleAfter
	failed := r.LastRun.State == "failed"
	if failed || r.Stale {
		levels = append(levels, LevelCritical)
	}
	r.Level = Worst(levels...)
	r.Headline = headline(r, failed)
}

func headline(r *Report, failed bool) string {
	if failed {
		if r.LastRun.Reason == "disk_full" {
			return "The last backup failed: the backup drive is full."
		}
		return "The last backup failed."
	}
	for _, d := range r.Drives {
		if d.Level == LevelCritical {
			return "The clinic's drive is almost full."
		}
	}
	if r.Backup.Level == LevelCritical {
		return "The next backup won't fit on the backup drive."
	}
	if r.Stale {
		return "No backup in over a day."
	}
	for _, d := range r.Drives {
		if d.Level == LevelLow {
			return "The clinic's drive is getting full."
		}
	}
	if r.Backup.Level == LevelLow {
		return "The backup drive is getting full."
	}
	return ""
}
