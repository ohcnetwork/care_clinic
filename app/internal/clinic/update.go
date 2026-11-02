package clinic

import (
	"errors"
	"fmt"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/compose"
)

type ChannelStatus struct {
	BackendBranch   string `json:"backend_branch"`
	FrontendBranch  string `json:"frontend_branch"`
	Backend         string `json:"backend"`
	Frontend        string `json:"frontend"`
	PendingBackend  string `json:"pending_backend"`
	PendingFrontend string `json:"pending_frontend"`
}

func (e *Clinic) ChannelStatus() ChannelStatus {
	lock := compose.ReadLock(e.InstallDir)
	return ChannelStatus{
		BackendBranch:   e.Pins.BeRef,
		FrontendBranch:  e.Pins.FeRef,
		Backend:         lock.Backend.Current,
		Frontend:        lock.Frontend.Current,
		PendingBackend:  lock.Backend.Next,
		PendingFrontend: lock.Frontend.Next,
	}
}

// CheckForUpdate only looks up the newest commits. Nothing is downloaded, built
// or restarted until the user installs the update.
func (e *Clinic) CheckForUpdate() (compose.Update, error) {
	return e.Builder().DiscoverUpdate()
}

func (e *Clinic) DeclineUpdate() error { return e.Builder().DeclineUpdate() }

const noEncryptedBackups = "this install cannot write encrypted backups, so the update was not applied - " +
	"recover the backup encryption certificate first"

// ApplyUpdate installs the update the user accepted. CARE is stopped before
// anything is downloaded, built or migrated and is started again afterwards.
// A failure before the new images are promoted starts CARE on its previous
// version; a later one is reported as is, because a database that has started
// migrating is never rolled back automatically.
func (e *Clinic) ApplyUpdate() error {
	b := e.Builder()
	waiting := b.Waiting()
	if !waiting.Any() {
		return errors.New("there is no update to install - check for updates first")
	}
	if pending, err := e.PendingPluginRecovery(); err != nil {
		return err
	} else if pending {
		return errors.New("a plugin rollback is unfinished; start CARE to recover it before updating")
	}
	if waiting.Backend != "" && !e.Backups().BackupEncryptionOn() {
		return errors.New(noEncryptedBackups)
	}
	e.logln("Installing the CARE update - CARE stops while it downloads and builds (patient data is kept)...")
	if err := e.StopStack(); err != nil {
		return err
	}
	if err := e.Backups().RecoverRestore(); err != nil {
		return err
	}
	var jobs []compose.Job
	if waiting.Backend != "" {
		jobs = append(jobs, compose.EnsureBackend)
	}
	if waiting.Frontend != "" {
		jobs = append(jobs, compose.EnsureFrontend)
	}
	if err := b.PendingAt(waiting).Sequence(jobs...); err != nil {
		return e.restartAfterFailedUpdate(err)
	}
	if waiting.Backend != "" {
		if err := e.backupBeforeUpdate(); err != nil {
			return e.restartAfterFailedUpdate(err)
		}
	}
	if _, err := b.ApplyPending(); err != nil {
		return e.restartAfterFailedUpdate(err)
	}
	if err := e.startStopped(nil); err != nil {
		return fmt.Errorf("the update was installed, but CARE did not come back up: %w", err)
	}
	b.PruneDangling()
	e.logln("CARE is up to date.")
	return nil
}

// restartAfterFailedUpdate starts CARE again on the version it had before,
// after a step failed while the images of the running version were untouched.
func (e *Clinic) restartAfterFailedUpdate(cause error) error {
	e.logln("The update was not installed; starting CARE again on the version it had before...")
	if err := e.startStopped(nil); err != nil {
		return errors.Join(cause, fmt.Errorf("CARE could not be started again on its previous version: %w", err))
	}
	return fmt.Errorf("the update was not installed and CARE is running as before: %w", cause)
}

func (e *Clinic) backupBeforeUpdate() error {
	if !e.Backups().BackupEncryptionOn() {
		return errors.New(noEncryptedBackups)
	}
	ts := time.Now().Format("20060102-150405")
	e.logln("Taking a safety backup before the update changes the database...")
	if err := e.run(nil, "docker", "compose", "run", "--rm", "-T", "backup", "once", "pre-update-"+ts); err != nil {
		return fmt.Errorf("couldn't take a safety backup, so the update was not applied: %w", err)
	}
	e.logln("Safety backup written: care-pre-update-" + ts + ".dump.enc")
	return nil
}
