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
	if err := e.Builder().Sequence(jobs...); err != nil {
		e.logln("The build failed; starting CARE again on the images it had before...")
		if upErr := e.dc("up", "-d", "--wait", "--wait-timeout", "300"); upErr != nil {
			return errors.Join(err, fmt.Errorf("CARE could not be started again on its previous images: %w", upErr))
		}
		return err
	}
	if err := e.Start(); err != nil {
		return err
	}
	e.logln(done)
	return nil
}
