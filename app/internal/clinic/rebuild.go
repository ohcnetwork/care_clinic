package clinic

import (
	"fmt"

	"github.com/ohcnetwork/care_desktop/app/internal/compose"
)

func (e *Clinic) RebuildBackend() error {
	if err := e.Backups().RecoverRestore(); err != nil {
		return err
	}
	if err := e.Builder().BuildBackend(); err != nil {
		return err
	}
	if err := e.stopWorkers(); err != nil {
		return err
	}
	if err := e.dc("up", "-d", "--wait", "--wait-timeout", "300", "backend"); err != nil {
		return fmt.Errorf("backend startup failed; workers and the scheduler remain stopped: %w", err)
	}
	if err := e.migrate(); err != nil {
		return err
	}
	e.syncFrontendPluginsOrWarn()
	if err := e.dc("up", "-d", "--wait", "--wait-timeout", "300", "celery-worker", "celery-beat"); err != nil {
		return err
	}
	e.logln("Backend rebuilt and restarted.")
	return nil
}

func (e *Clinic) RebuildFrontend() error {
	if err := e.Backups().RecoverRestore(); err != nil {
		return err
	}
	if err := e.Builder().BuildFrontend(); err != nil {
		return err
	}
	if err := e.dc("up", "-d", "--wait", "--wait-timeout", "300", "frontend"); err != nil {
		return err
	}
	e.logln("Frontend rebuilt and restarted.")
	return nil
}

func (e *Clinic) RebuildAll() error {
	if err := e.Backups().RecoverRestore(); err != nil {
		return err
	}
	e.logln("Rebuilding CARE and restarting every service (patient data is kept)...")
	if err := e.Builder().Parallel(compose.BuildBackend, compose.BuildFrontend); err != nil {
		return err
	}
	if err := e.Restart(); err != nil {
		return err
	}
	e.logln("Everything rebuilt and restarted.")
	return nil
}
