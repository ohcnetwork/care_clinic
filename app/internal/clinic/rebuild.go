package clinic

import (
	"errors"
	"fmt"

	"github.com/ohcnetwork/care_desktop/app/internal/compose"
)

func (e *Clinic) RebuildBackend() error {
	return e.rebuild("Rebuilding the CARE backend", "Backend rebuilt and restarted.", compose.BuildBackend)
}

func (e *Clinic) RebuildFrontend() error {
	return e.rebuild("Rebuilding the CARE frontend", "Frontend rebuilt and restarted.", compose.BuildFrontend)
}

func (e *Clinic) RebuildAll() error {
	return e.rebuild("Rebuilding CARE", "Everything rebuilt and restarted.", compose.BuildBackend, compose.BuildFrontend)
}

func (e *Clinic) rebuild(start, done string, jobs ...compose.Job) error {
	if err := e.Backups().RecoverRestore(); err != nil {
		return err
	}
	e.logln(start + " - CARE stops while it builds so the build has the Docker VM's memory (patient data is kept)...")
	if err := e.dc("down"); err != nil {
		return err
	}
	b := e.Builder()
	backendRebuilt := false
	for _, job := range jobs {
		if err := b.Sequence(job); err != nil {
			return e.restartAfterFailedBuild(err, backendRebuilt)
		}
		if job.Label == compose.BuildBackend.Label {
			backendRebuilt = true
		}
	}
	if err := e.Start(); err != nil {
		return err
	}
	e.logln(done)
	return nil
}

func (e *Clinic) restartAfterFailedBuild(buildErr error, backendRebuilt bool) error {
	if !backendRebuilt {
		e.logln("The build failed; starting CARE again on the images it had before...")
		if err := e.dc("up", "-d", "--wait", "--wait-timeout", "300"); err != nil {
			return errors.Join(buildErr, fmt.Errorf("CARE could not be started again on its previous images: %w", err))
		}
		return buildErr
	}
	e.logln("A later build failed after the backend was rebuilt; migrating the database before starting CARE again...")
	if err := e.dc("up", "-d", "--wait", "--wait-timeout", "300", "db", "redis", "backend"); err != nil {
		return errors.Join(buildErr, fmt.Errorf("the rebuilt backend could not be started; workers and the scheduler remain stopped: %w", err))
	}
	if err := e.migrate(); err != nil {
		return errors.Join(buildErr, err)
	}
	if err := e.dc("up", "-d", "--wait", "--wait-timeout", "300"); err != nil {
		return errors.Join(buildErr, fmt.Errorf("CARE could not be started again after the backend rebuild: %w", err))
	}
	return buildErr
}
