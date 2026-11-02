package clinic

import (
	"fmt"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/compose"
	"github.com/ohcnetwork/care_desktop/app/internal/health"
)

// Start stops whatever is left of the CARE project, then rebuilds anything out
// of date and starts the clinic from that clean, stopped state.
func (e *Clinic) Start() error {
	if err := e.StopStack(); err != nil {
		return err
	}
	return e.startStopped(e.Refresh)
}

// recoverStopped finishes an interrupted restore and then runs refresh, unless a
// restore was pending: its files must stay exactly as the journal expects.
// Every CARE container must already be stopped.
func (e *Clinic) recoverStopped(refresh func() error) error {
	restoring, err := e.Backups().PendingRestore()
	if err != nil {
		return err
	}
	if err := e.Backups().RecoverRestore(); err != nil {
		return err
	}
	if refresh == nil || restoring {
		return nil
	}
	return refresh()
}

// startStopped brings the clinic up from a fully stopped state.
func (e *Clinic) startStopped(refresh func() error) error {
	if recovered, err := e.RecoverPlugins(); err != nil || recovered {
		return err
	}
	if err := e.recoverStopped(refresh); err != nil {
		return err
	}
	if err := health.EnsurePortFree(e.Runner(), e.host()); err != nil {
		return err
	}
	if err := e.Builder().Sequence(compose.EnsureBackend, compose.EnsureFrontend,
		compose.EnsureBackup, compose.EnsureCaddy); err != nil {
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
	if err := e.waitForCARE(); err != nil {
		return err
	}
	if err := e.Backups().FinishRestore(); err != nil {
		return err
	}
	e.logln("")
	e.logln("CARE is up -> https://" + e.host() + "/   (login: admin)")
	if !e.localSetupOffered {
		e.setUpThisComputer()
	}
	return nil
}

func (e *Clinic) waitForCARE() error {
	if e.startReady != nil {
		return e.startReady()
	}
	return health.Wait(e.Log, 3*time.Minute)
}
