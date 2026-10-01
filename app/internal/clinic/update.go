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

func (e *Clinic) CheckForUpdate() (compose.Update, error) {
	return e.Builder().PrepareUpdate()
}

func (e *Clinic) DeclineUpdate() error { return e.Builder().DeclineUpdate() }

func (e *Clinic) ApplyUpdate() error {
	if err := e.Backups().RecoverRestore(); err != nil {
		return err
	}
	b := e.Builder()
	waiting := b.Waiting()
	if !waiting.Any() {
		return errors.New("there is no prepared update to apply")
	}
	if waiting.Backend != "" {
		if err := e.backupBeforeUpdate(); err != nil {
			return err
		}
		if err := e.stopWorkers(); err != nil {
			return err
		}
	}
	applied, err := b.ApplyPending()
	if err != nil {
		return err
	}
	if !applied.Any() {
		return errors.New("the prepared update is no longer available - it will be built again on the next check")
	}
	if applied.Backend != "" {
		if err := e.dc("up", "-d", "--wait", "--wait-timeout", "300", "backend"); err != nil {
			return fmt.Errorf("the updated backend did not start; workers and the scheduler remain stopped: %w", err)
		}
		e.logln("Applying database migrations...")
		if err := e.migrate(); err != nil {
			return err
		}
	}
	if err := e.dc("up", "-d", "--wait", "--wait-timeout", "300"); err != nil {
		return err
	}
	b.PruneDangling()
	e.logln("CARE is up to date.")
	return nil
}

func (e *Clinic) applyStagedUpdate() error {
	b := e.Builder()
	waiting := b.Waiting()
	if !waiting.Any() {
		return nil
	}
	if !e.Backups().BackupEncryptionOn() {
		e.logln("A CARE update is ready, but this install cannot write encrypted backups, " +
			"so it was not applied. Recover the backup encryption certificate before applying updates.")
		return nil
	}
	if waiting.Backend != "" {
		if err := e.backupBeforeUpdate(); err != nil {
			e.logln("The CARE update was not applied: " + err.Error())
			return nil
		}
	}
	_, err := b.ApplyPending()
	return err
}

func (e *Clinic) backupBeforeUpdate() error {
	if !e.Backups().BackupEncryptionOn() {
		return errors.New("this install cannot write encrypted backups, so the update was not applied - " +
			"recover the backup encryption certificate first")
	}
	ts := time.Now().Format("20060102-150405")
	e.logln("Taking a safety backup before the update changes the database...")
	if err := e.run(nil, "docker", "compose", "run", "--rm", "-T", "backup", "once", "pre-update-"+ts); err != nil {
		return fmt.Errorf("couldn't take a safety backup, so the update was not applied: %w", err)
	}
	e.logln("Safety backup written: care-pre-update-" + ts + ".dump.enc")
	return nil
}
