package main

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

type SetupFailure struct {
	CanRetry            bool `json:"can_retry"`
	DownloadInterrupted bool `json:"download_interrupted"`
}

type setupAttempt struct {
	config   Config
	prepare  func() error
	start    func() error
	prepared bool
}

func (s *setupAttempt) run() error {
	if !s.prepared {
		if err := s.prepare(); err != nil {
			return err
		}
		s.prepared = true
	}
	return s.start()
}

func (a *App) setupFailure(err error) SetupFailure {
	cfg := a.loadConfig()
	var network *proc.NetworkError
	return SetupFailure{
		CanRetry:            a.setupAttempt != nil && a.setupAttempt.config == cfg && cfg.Role == roleServer && cfg.AdminPwHash != "" && !cfg.SetupDone && !cfg.Removing,
		DownloadInterrupted: errors.As(err, &network),
	}
}

// RetrySetup resumes only the attempt this process already validated and prepared.
// It never accepts replacement settings or runs failed-install cleanup.
func (a *App) RetrySetup() (err error) {
	defer a.logError(&err)
	if err := a.requireServer(); err != nil {
		return err
	}
	if err := a.lockJob(); err != nil {
		return err
	}
	if err := a.validateSetupRetry(); err != nil {
		a.jobMu.Unlock()
		return err
	}
	return a.runLockedJob(a.setupAttempt.run, true, "setup")
}

func (a *App) validateSetupRetry() error {
	cfg := a.loadConfig()
	if cfg.SetupDone || cfg.Removing || a.setupAttempt == nil || cfg.AdminPwHash == "" {
		return errors.New("there is no unfinished installation that can be retried in this session")
	}
	if cfg != a.setupAttempt.config {
		return errors.New("the saved setup changed; return to setup before trying again")
	}
	recovery := setupRecoveryStatus(cfg)
	if !recovery.BackupVerified || !recovery.CodesSaved || recovery.CodesProblem != "" {
		return errors.New("the saved recovery files are unavailable or changed; reconnect their location before trying again")
	}
	parent := ""
	if cfg.BackupDir != "" {
		parent = filepath.Dir(cfg.BackupDir)
	}
	if problem := a.ValidateBackupDir(parent); problem != "" {
		return fmt.Errorf("the backup location is unavailable: %s", problem)
	}
	return nil
}
