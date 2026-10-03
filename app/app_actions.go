package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/ohcnetwork/care_desktop/app/internal/clinic"
	"github.com/ohcnetwork/care_desktop/app/internal/health"
	"github.com/ohcnetwork/care_desktop/app/internal/prereq"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/autostart"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/mdns"

	"golang.org/x/crypto/bcrypt"
)

func (a *App) run(fn func() error, markSetup bool, label string) error {
	if err := a.requireServer(); err != nil {
		return a.logged(err)
	}
	return a.runJob(fn, markSetup, label)
}

func (a *App) runJob(fn func() error, markSetup bool, label string) error {
	if err := a.lockJob(); err != nil {
		return a.logged(err)
	}
	return a.runLockedJob(fn, markSetup, label)
}

// The caller owns jobMu; ownership passes to the worker.
func (a *App) runLockedJob(fn func() error, markSetup bool, label string) error {
	a.activeJob.Store(label)
	go func() {
		defer func() {
			a.activeJob.Store("")
			a.jobMu.Unlock()
		}()
		code := 0
		defer func() {
			if r := recover(); r != nil {
				code = 1
				a.log.Writef("PANIC in %s: %v\n%s", label, r, debug.Stack())
				detail := fmt.Sprintf("CARE hit an internal error during %s: %v", label, r)
				a.logln("error: " + detail)
				if !markSetup {
					a.reportError(failureTitle(label), detail)
				} else {
					a.setupAttempt = nil
					a.emit("setup-failed", SetupFailure{})
				}
			}
			a.emit("care-done", code, label)
		}()
		err := fn()
		if markSetup && err == nil {
			cfg := a.loadConfig()
			cfg.SetupDone = true
			err = a.saveConfig(cfg)
			if err == nil {
				a.setupAttempt = nil
				if aerr := autostart.Set(true); aerr != nil {
					a.logln("note: couldn't set CARE Desktop to open at login (" + aerr.Error() +
						") - turn on \"Start at login\" yourself")
				}
				a.emit("setup-done", true)
			}
		}
		if err != nil {
			a.logln("error: " + err.Error())
			code = 1
			if !markSetup {
				a.reportError(failureTitle(label), err.Error())
			} else {
				a.emit("setup-failed", a.setupFailure(err))
			}
		}
	}()
	return nil
}

func (a *App) lockJob() error {
	if !a.jobMu.TryLock() {
		return errors.New("something else is still running - wait for it to finish")
	}
	if a.closing {
		a.jobMu.Unlock()
		return errors.New("CARE Desktop is closing")
	}
	return nil
}

// withJob and withReadJob log whatever they return. Every bound method that
// runs through them therefore leaves its failure in the log file, named, with
// no line of its own to forget.
func (a *App) withJob(fn func() error) error {
	if err := a.lockJob(); err != nil {
		return a.logged(err)
	}
	defer a.jobMu.Unlock()
	return a.logged(fn())
}

func (a *App) withServerJob(fn func() error) error {
	return a.withJob(func() error {
		if err := a.requireServer(); err != nil {
			return err
		}
		return fn()
	})
}

func (a *App) withLabeledJob(label string, fn func() error) error {
	return a.withServerJob(func() error {
		a.activeJob.Store(label)
		defer a.activeJob.Store("")
		return fn()
	})
}

func (a *App) withReadJob(fn func() error) error {
	if !a.jobMu.TryRLock() {
		return a.logged(errors.New("something else is still running - wait for it to finish"))
	}
	defer a.jobMu.RUnlock()
	if a.closing {
		return a.logged(errors.New("CARE Desktop is closing"))
	}
	return a.logged(fn())
}

func (a *App) requireSetup() error {
	if err := a.requireServer(); err != nil {
		return err
	}
	cfg := a.loadConfig()
	if cfg.Removing {
		return errors.New("cleanup is incomplete; finish removing this installation before starting or changing it")
	}
	if !cfg.SetupDone {
		return errors.New("not set up yet - run the first-time setup")
	}
	info, err := os.Stat(filepath.Join(a.installDir(), "docker-compose.yml"))
	if err != nil {
		return fmt.Errorf("the installed compose file is unavailable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("the installed compose file is not a regular file")
	}
	return nil
}

func (a *App) beginRemoval() error {
	if err := a.requireServer(); err != nil {
		return err
	}
	cfg := a.loadConfig()
	cfg.Removing = true
	if err := a.saveConfig(cfg); err != nil {
		return err
	}
	a.setupAttempt = nil
	return a.restartAdvertise()
}

func (a *App) requireAdmin(password string) error {
	if err := a.requireServer(); err != nil {
		return err
	}
	if !a.loadConfig().SetupDone || !a.VerifyAdminPassword(password) {
		return errors.New("the Desktop admin password does not match this installation")
	}
	return nil
}

func (a *App) requireStableClinic() error {
	if err := a.requireSetup(); err != nil {
		return err
	}
	pending, err := a.engine().Backups().PendingRestore()
	if err != nil {
		return err
	}
	if pending {
		return errors.New("a restore is unfinished; start CARE to recover it before making other changes")
	}
	return nil
}

// failureTitle names the failure in the words the operator used to press,
// so a care-error event can be shown without the desktop re-deriving it.
func failureTitle(label string) string {
	switch label {
	case "start":
		return "CARE couldn't start"
	case "restart":
		return "CARE couldn't restart"
	case "stop":
		return "CARE couldn't stop"
	case "restore":
		return "Restore didn't finish"
	case "backup-now":
		return "Backup didn't finish"
	case "rebuild-all", "rebuild-backend", "rebuild-frontend":
		return "Rebuild didn't finish"
	case "apply-plugins":
		return "The plugins couldn't be applied"
	case "update":
		return "The CARE update didn't finish"
	case "free-space":
		return "Cleanup didn't finish"
	case "app-update":
		return "The CARE Desktop update didn't finish"
	case "network-name":
		return "The clinic address is already in use"
	}
	return "CARE couldn't finish that"
}

func (a *App) ClinicAction(action, adminPassword string) (err error) {
	defer a.logError(&err)
	switch action {
	case "start", "stop", "restart", "rebuild-all", "rebuild-backend", "rebuild-frontend", "apply-plugins", "backup-now", "update", "free-space":
	default:
		return errors.New("action not allowed: " + action)
	}
	return a.run(func() error {
		if err := a.requireSetup(); err != nil {
			return err
		}
		if action != "start" && action != "stop" {
			if err := a.requireStableClinic(); err != nil {
				return err
			}
		}
		if strings.HasPrefix(action, "rebuild-") {
			if err := a.requireAdmin(adminPassword); err != nil {
				return err
			}
		}
		if action != "stop" {
			if err := a.ensureDockerReady(); err != nil {
				return err
			}
		}
		if action == "rebuild-all" {
			if err := a.syncInstallKit(); err != nil {
				return err
			}
		}
		return actionFunc(a.engine(), action)()
	}, false, action)
}

// ensureDockerReady stops a stopped container engine from reaching the clinic
// operations as an unexplained subprocess failure such as "inspect local
// images: exit status 1". The plan decides what can be offered, so this names
// whichever engine the platform actually uses. Stop is excluded: it needs no
// engine to report that a clinic which cannot be reached is not running.
func (a *App) ensureDockerReady() error {
	p := a.provisioner()
	plan := p.DockerPlan()
	if plan.Action == prereq.ActionNone {
		return nil
	}
	status := a.DockerStatus()
	if plan.Action != prereq.ActionOpen {
		return fmt.Errorf("%s CARE needs it to run the clinic; set it up from the requirements check", status.Message)
	}
	if !a.confirmDialog(plan.Label+"?", status.Message+"\n\nCARE needs it to run the clinic. "+
		"Start it now and wait for it to be ready?") {
		return errors.New(status.Message)
	}
	return p.OpenDocker()
}

func actionFunc(e *clinic.Clinic, action string) func() error {
	switch action {
	case "start":
		return e.Start
	case "stop":
		return e.Stop
	case "restart":
		return e.Restart
	case "rebuild-all":
		return e.RebuildAll
	case "rebuild-backend":
		return e.RebuildBackend
	case "rebuild-frontend":
		return e.RebuildFrontend
	case "apply-plugins":
		return e.ApplyPlugins
	case "backup-now":
		return e.BackupNow
	case "update":
		return e.ApplyUpdate
	case "free-space":
		return e.FreeSpace
	}
	return nil
}

func (a *App) RunSetup(mdnsName, adminPassword, backupDir string) (err error) {
	defer a.logError(&err)
	if err := ValidatePassword(adminPassword); err != nil {
		return err
	}

	host := mdns.Label(mdnsName)
	if host == "" {
		host = "care"
	}

	if err := mdns.ValidateLabel(host); err != nil {
		return err
	}
	if err := a.requireServer(); err != nil {
		return err
	}
	if err := a.lockJob(); err != nil {
		return err
	}
	issues := a.validateSetup(host, adminPassword, backupDir)
	if len(issues) > 0 {
		a.jobMu.Unlock()
		return fmt.Errorf("setup needs attention (%s): %s", issues[0].Step, issues[0].Message)
	}
	return a.runLockedJob(func() error {
		cfg := a.loadConfig()
		if cfg.SetupDone || cfg.Removing {
			return errors.New("this computer already has a clinic set up")
		}
		if cfg.BackupCertificate == "" || !cfg.BackupRecoveryVerified || cfg.adminRecoveryCount() != 6 {
			return errors.New("save and verify the backup recovery file and save your six Desktop admin recovery codes before installing")
		}
		if err := a.recoveryLocation(cfg.BackupRecoveryPath, backupDir); err != nil {
			return err
		}
		if err := mdns.CheckAvailable(host); err != nil {
			return err
		}
		cfg.MDNSName = host + ".local"
		h, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("couldn't secure the admin password: %w", err)
		}
		cfg.AdminPwHash = string(h)
		if err := prepareSetupBackupKey(&cfg, adminPassword); err != nil {
			return err
		}
		if strings.TrimSpace(backupDir) != "" {
			cfg.BackupDir = filepath.Join(strings.TrimSpace(backupDir), "care-db-backups")
		}
		backupParent := strings.TrimSpace(backupDir)
		if backupParent == "" && cfg.BackupDir != "" {
			backupParent = filepath.Dir(cfg.BackupDir)
		}
		if problem := a.ValidateBackupDir(backupParent); problem != "" {
			return errors.New(problem)
		}
		if err := a.saveConfig(cfg); err != nil {
			return err
		}
		var e *clinic.Clinic
		a.setupAttempt = &setupAttempt{
			config: cfg,
			prepare: func() error {
				if _, err := a.ensureInstallDir(); err != nil {
					return err
				}
				e = a.engine()
				e.AdminPassword = adminPassword
				e.BackupCertificate = cfg.BackupCertificate
				if err := health.EnsurePortFree(e.Runner(), e.Host()); err != nil {
					return err
				}
				return e.Setup()
			},
			start: func() error {
				if err := a.restartAdvertise(); err != nil {
					return err
				}
				return e.Start()
			},
		}
		return a.setupAttempt.run()
	}, true, "setup")
}

func (a *App) CleanupFailedInstall() error {
	return a.withServerJob(func() error {
		cfg := a.loadConfig()
		if cfg.SetupDone {
			return errors.New("this clinic is installed; use Uninstall instead of failed-install cleanup")
		}
		e := a.engine()
		if _, err := a.ScanResidue(); err != nil {
			return err
		}
		if err := e.Backups().PreserveBackupCertificate(); err != nil {
			return err
		}
		if err := a.beginRemoval(); err != nil {
			return err
		}
		if err := e.Uninstall(clinic.UninstallOptions{
			RemoveInstallDir:        true,
			RemoveUnusedCertificate: true,
		}); err != nil {
			return err
		}
		if err := a.forgetConfig(); err != nil {
			return err
		}
		if err := a.restartAdvertise(); err != nil {
			return err
		}
		return a.keepChosenName(cfg)
	})
}

func (a *App) ClinicStatus() (status string, err error) {
	defer a.logError(&err)
	if err := a.requireServer(); err != nil {
		return "", err
	}
	return a.engine().Status()
}
