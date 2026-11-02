package main

import (
	"fmt"
	"os"

	"github.com/ohcnetwork/care_desktop/app/internal/clinic"
)

func (a *App) envPath(name string) (string, error) {
	return clinic.EnvFilePath(a.installDir(), name)
}

func (a *App) ReadEnv(name, adminPassword string) (string, error) {
	var content string
	err := a.withReadJob(func() error {
		if err := a.requireAdmin(adminPassword); err != nil {
			return err
		}
		if err := a.requireSetup(); err != nil {
			return err
		}
		p, err := a.envPath(name)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		content = string(b)
		return err
	})
	return content, err
}

// ApplyEnvChanges changes the clinic settings and starts CARE on them as one
// job. A request that cannot work is turned away here, before anything runs.
// Once accepted, CARE stops before any file changes, and the previous settings
// are put back if CARE does not start on the new ones.
func (a *App) ApplyEnvChanges(changes []clinic.EnvChange, adminPassword string) (err error) {
	defer a.logError(&err)
	if err := a.requireServer(); err != nil {
		return err
	}
	if err := a.lockJob(); err != nil {
		return err
	}
	if err := a.checkEnvChanges(changes, adminPassword); err != nil {
		a.jobMu.Unlock()
		return err
	}
	return a.runLockedJob(func() error {
		release, err := holdUpdateChecks()
		if err != nil {
			return fmt.Errorf("nothing was changed: %w", err)
		}
		defer release()
		if err := a.ensureDockerReady(); err != nil {
			return fmt.Errorf("nothing was changed: %w", err)
		}
		return a.engine().ApplyEnvFiles(changes)
	}, false, "apply-settings")
}

func (a *App) checkEnvChanges(changes []clinic.EnvChange, adminPassword string) error {
	if err := a.requireAdmin(adminPassword); err != nil {
		return err
	}
	if err := a.requireStableClinic(); err != nil {
		return err
	}
	return a.engine().CheckEnvFiles(changes)
}
