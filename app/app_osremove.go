package main

import (
	"errors"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/appremoval"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	uninstallFlag      = "--uninstall"
	uninstallCheckFlag = "--uninstall-check"

	otherInstanceWait = 10 * time.Second
)

const (
	exitRemovable = 0
	exitSetUp     = 1
	exitRunning   = 3
	exitOtherUser = 4
)

func removalPreflight() (int, bool) {
	if appremoval.OtherInstanceRunning(otherInstanceWait) {
		return exitRunning, true
	}
	if appremoval.ForeignSession() {
		return exitOtherUser, true
	}
	return 0, false
}

func (a *App) setUp() (bool, error) {
	cfg := a.loadConfig()
	if cfg.Role == "" {
		return false, nil
	}
	installed, err := a.installDirInUse()
	if err != nil {
		return true, err
	}
	wizard := Config{
		Role: cfg.Role, MDNSName: cfg.MDNSName,
		BackupCertificate: cfg.BackupCertificate, BackupRecoveryPath: cfg.BackupRecoveryPath,
		BackupRecoveryVerified: cfg.BackupRecoveryVerified, AdminRecoveryHashes: cfg.AdminRecoveryHashes,
		AdminRecoveryPath: cfg.AdminRecoveryPath,
	}
	return installed || cfg != wizard, nil
}

func (a *App) removalExitCode() int {
	if set, err := a.setUp(); err != nil || set {
		return exitSetUp
	}
	return exitRemovable
}

func (a *App) UninstallRequested() bool { return a.osUninstall }

func (a *App) ExitUninstall() {
	go a.quitWhenIdle()
}

func (a *App) CanRemoveApp() bool {
	if a.osUninstall {
		return false
	}
	_, err := appremoval.Target()
	return err == nil
}

func (a *App) RemoveApp() (err error) {
	defer a.logError(&err)
	if a.osUninstall {
		return errors.New("the uninstaller is already removing CARE Desktop")
	}
	a.jobMu.Lock()
	defer a.jobMu.Unlock()
	if a.closing {
		return errors.New("CARE Desktop is closing")
	}
	set, err := a.setUp()
	if err != nil {
		return err
	}
	if set {
		return errors.New("uninstall the clinic setup or disconnect this computer before removing the app")
	}
	target, err := appremoval.Target()
	if err != nil {
		return err
	}
	a.removeTarget = target
	go a.quitWhenIdle()
	return nil
}

func (a *App) quitWhenIdle() {
	a.jobMu.Lock()
	a.closing = true
	a.jobMu.Unlock()
	wruntime.Quit(a.ctx)
}
