package main

import (
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/health"
	"github.com/ohcnetwork/care_desktop/app/internal/storage"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/diskspace"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	storageFirstCheck = 20 * time.Second
	storageInterval   = 5 * time.Minute
)

type storageWatch struct {
	mu         sync.Mutex
	report     storage.Report
	checked    bool
	alerted    string
	notifyOnce sync.Once
	notifyOK   bool
}

func (a *App) DiskStatus() storage.InstallResult {
	docker, err := diskspace.Of(a.dockerDataDir())
	if err != nil {
		return storage.InstallResult{OK: true, Message: "Couldn't measure free space (" + err.Error() + ")"}
	}
	if a.loadConfig().SetupDone {
		return storage.CheckRunning(docker)
	}
	install, err := diskspace.Of(a.installDir())
	if err != nil {
		install = docker
	}
	return storage.CheckInstall(install, docker)
}

func (a *App) dockerDataDir() string {
	if runtime.GOOS == "linux" {
		if dir, err := a.engine().DockerRootDir(); err == nil {
			return dir
		}
	}
	return storage.DockerDataDir()
}

func (a *App) StorageStatus() storage.Report {
	a.store.mu.Lock()
	report, checked := a.store.report, a.store.checked
	a.store.mu.Unlock()
	if checked {
		return report
	}
	return a.RecheckStorage()
}

func (a *App) RecheckStorage() storage.Report {
	report := a.checkStorage()
	a.store.mu.Lock()
	a.store.report, a.store.checked = report, true
	a.store.mu.Unlock()
	a.emit("care-storage", report)
	a.alertStorage(report)
	return report
}

func (a *App) BackupDirSpace(dir string) storage.BackupSpace {
	target := a.engine().BackupDirPath()
	if dir != "" && filepath.IsAbs(dir) {
		target = filepath.Join(dir, "care-db-backups")
	}
	b, _ := a.backupSpace(target)
	return b
}

func (a *App) backupSpace(target string) (storage.BackupSpace, error) {
	e := a.engine()
	u, err := diskspace.Of(target)
	if err != nil {
		return storage.BackupSpace{Dir: target, DaysLeft: -1, Level: storage.LevelUnknown,
			Message: "Couldn't measure free space: " + err.Error()}, err
	}
	daily, _, found, err := storage.LatestSets(e.BackupDirPath())
	if err != nil {
		found = false
	}
	shares := false
	if docker, err := diskspace.Of(a.dockerDataDir()); err == nil {
		shares = docker.Volume == u.Volume
	}
	return storage.AssessBackup(target, u, daily, found, e.BackupKeepsForever(), shares), nil
}

func (a *App) backupSpaceProblem(target string) string {
	b, err := a.backupSpace(target)
	if err != nil || b.Level != storage.LevelCritical {
		return ""
	}
	return b.Message + " Choose a folder on a drive with more free space."
}

func (a *App) checkStorage() storage.Report {
	now := time.Now()
	r := storage.Report{CheckedAt: now.Unix(), Level: storage.LevelOK}
	cfg := a.loadConfig()
	if cfg.Role != roleServer || !cfg.SetupDone {
		return r
	}
	e := a.engine()
	running := health.Ping().Active
	if docker, err := diskspace.Of(a.dockerDataDir()); err == nil {
		d := storage.AssessDrive("docker", "Clinic data", docker)
		d.Cleanable = running && runtime.GOOS == "linux"
		if runtime.GOOS != "linux" {
			d.Label = "This computer's drive"
			if d.Level == storage.LevelOK {
				d.Message = "Plenty of room for Rancher Desktop's disk to grow."
			}
		}
		r.Drives = append(r.Drives, d)
	}
	if running && runtime.GOOS != "linux" {
		if free, total, err := e.DockerDiskFree(); err == nil {
			d := storage.AssessDrive("vm", "Rancher Desktop disk",
				diskspace.Usage{Path: "Rancher Desktop", Free: free, Total: total})
			d.Cleanable = true
			r.Drives = append(r.Drives, d)
		}
	}
	r.Backup, _ = a.backupSpace(e.BackupDirPath())
	if run, err := storage.ReadBackupRun(filepath.Join(a.installDir(), backupStateDir)); err == nil {
		r.LastRun = run
	}
	if _, newest, found, err := storage.LatestSets(e.BackupDirPath()); err == nil && found {
		r.NewestBackupAt = newest.At.Unix()
	}
	storage.Summarise(&r, running, now)
	return r
}

func (a *App) watchStorage() {
	timer := time.NewTimer(storageFirstCheck)
	defer timer.Stop()
	for {
		select {
		case <-a.advStop:
			return
		case <-timer.C:
			if a.loadConfig().SetupDone {
				a.RecheckStorage()
			}
			timer.Reset(storageInterval)
		}
	}
}

func (a *App) alertStorage(r storage.Report) {
	a.store.mu.Lock()
	if r.Level != storage.LevelCritical {
		a.store.alerted = ""
		a.store.mu.Unlock()
		return
	}
	fresh := r.Headline != a.store.alerted
	a.store.alerted = r.Headline
	a.store.mu.Unlock()
	if !fresh {
		return
	}
	a.logln("storage: " + r.Headline)
	if a.ctx == nil || !a.notificationsReady() {
		return
	}
	_ = wruntime.SendNotification(a.ctx, wruntime.NotificationOptions{
		ID:    "care-storage",
		Title: "CARE needs attention",
		Body:  r.Headline + " Open CARE Clinic to fix it.",
	})
}

func (a *App) notificationsReady() bool {
	a.store.notifyOnce.Do(func() {
		if wruntime.InitializeNotifications(a.ctx) != nil || !wruntime.IsNotificationAvailable(a.ctx) {
			return
		}
		ok, err := wruntime.CheckNotificationAuthorization(a.ctx)
		if err == nil && !ok {
			ok, err = wruntime.RequestNotificationAuthorization(a.ctx)
		}
		a.store.notifyOK = err == nil && ok
	})
	return a.store.notifyOK
}
