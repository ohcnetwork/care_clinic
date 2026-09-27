package clinic

import (
	"fmt"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/health"
)

func (e *Clinic) Start() error {
	if err := e.Backups().RecoverRestore(); err != nil {
		return err
	}
	if err := health.EnsurePortFree(e.Runner(), e.host()); err != nil {
		return err
	}
	if err := e.applyStagedUpdate(); err != nil {
		return err
	}
	if err := e.Builder().EnsureBackendImage(); err != nil {
		return err
	}
	if err := e.Builder().EnsureFrontendImage(); err != nil {
		return err
	}
	if err := e.Builder().EnsureBackupImage(); err != nil {
		return err
	}
	if err := e.Builder().EnsureCaddyImage(); err != nil {
		return err
	}
	if err := e.Backups().EnsureKeysDir(); err != nil {
		return err
	}
	if err := e.stopWorkers(); err != nil {
		return err
	}
	e.logln("Starting CARE...")
	if err := e.dc("up", "-d", "--wait", "--wait-timeout", "300", "db", "redis", "backend"); err != nil {
		return fmt.Errorf("backend startup failed; workers and the scheduler remain stopped: %w", err)
	}
	e.logln("Applying database migrations...")
	if err := e.migrate(); err != nil {
		return err
	}
	if err := e.createAdmin(); err != nil {
		return fmt.Errorf("startup failed; workers and the scheduler remain stopped: %w", err)
	}
	e.syncFrontendPluginsOrWarn()
	if err := e.dc("up", "-d", "--wait", "--wait-timeout", "300"); err != nil {
		return err
	}
	if err := e.dc("up", "-d", "--wait", "--wait-timeout", "300", "--no-deps", "--force-recreate", "caddy"); err != nil {
		return fmt.Errorf("the proxy could not be refreshed with the current configuration: %w", err)
	}
	e.logln("Waiting for CARE to become healthy...")
	if err := health.Wait(e.Log, 3*time.Minute); err != nil {
		return err
	}
	if err := e.Backups().FinishRestore(); err != nil {
		return err
	}
	e.logln("")
	e.logln("CARE is up -> https://" + e.host() + "/   (login: admin)")
	e.setUpThisComputer()
	return nil
}
