package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/vm"
)

// EngineState tells the panel whether the CARE engine matches this build of
// CARE Clinic and whether it is collecting guest and container logs.
type EngineState struct {
	Supported        bool   `json:"supported"`
	ExpectedVersion  string `json:"expected_version"`
	InstalledVersion string `json:"installed_version"`
	RunningVersion   string `json:"running_version"`
	Running          bool   `json:"running"`
	UpdateNeeded     bool   `json:"update_needed"`
	// UpdateFailed is set when the last update to ExpectedVersion was rolled
	// back (or is waiting to be), so the panel offers a retry instead of
	// updating again automatically. UpdateError says why.
	UpdateFailed bool   `json:"update_failed"`
	UpdateError  string `json:"update_error"`
	// Logs is "connecting", "collecting" or "interrupted" when running; an
	// empty value while running means an older helper that cannot report it.
	Logs string `json:"logs"`
}

func engineState(installed, expected string, toolsOK bool, s vm.HostStatus, u *vm.EngineUpdate) EngineState {
	st := EngineState{
		Supported: true, ExpectedVersion: expected, InstalledVersion: installed,
		Running: s.PID != 0, Logs: s.Logs,
	}
	if st.Running {
		st.RunningVersion = s.Version
	}
	if installed != "" && expected != "" {
		st.UpdateNeeded = installed != expected || !toolsOK ||
			(st.Running && st.RunningVersion != expected)
	}
	if u != nil && u.To == expected && (st.UpdateNeeded || u.Interrupted()) {
		st.UpdateNeeded, st.UpdateFailed, st.UpdateError = true, true, u.Error
		if u.Interrupted() {
			st.UpdateError = "the update didn't finish; the previous engine is restored before CARE starts"
		}
	}
	return st
}

func (a *App) EngineStatus() EngineState {
	if !vm.Supported() {
		return EngineState{}
	}
	installed, expected, toolsOK := a.provisioner().EngineVersions()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var pending *vm.EngineUpdate
	if u, ok, err := vm.PendingEngineUpdate(); err != nil {
		a.log.Writef("engine update record: %v", err)
	} else if ok {
		pending = &u
	}
	return engineState(installed, expected, toolsOK, vm.Status(ctx), pending)
}

// recoverEngineUpdate rolls back an engine update that the app was closed or
// the computer lost power during, before anything else uses the engine.
func (a *App) recoverEngineUpdate() {
	if !vm.Supported() {
		return
	}
	if _, ok, _ := vm.PendingEngineUpdate(); !ok {
		return
	}
	if err := a.withJob(a.provisioner().RecoverEngineUpdate); err != nil {
		a.logln("error: " + err.Error())
	}
}

// updateEngine runs inside a clinic job, automatically on the first launch
// after an app update or from the panel's retry button. The new engine is
// downloaded, verified and unpacked first; only then are the containers
// removed (volumes kept) so they are recreated on the new engine with its
// logging settings. If the new engine does not start, the previous engine and
// tools are restored and the clinic is started again on them. Once the new
// engine and Docker answer the update is kept; a CARE start failure after
// that is handled like any other start failure, not by changing the engine.
func (a *App) updateEngine() error {
	if !vm.Supported() {
		return errors.New("this computer runs CARE on Docker directly; there is no CARE engine to update")
	}
	p := a.provisioner()
	if err := p.RecoverEngineUpdate(); err != nil {
		return err
	}
	if err := vm.ClearFailedEngineUpdate(); err != nil {
		return err
	}
	st := a.EngineStatus()
	if !st.UpdateNeeded {
		a.logln("The CARE engine is already up to date.")
		return nil
	}
	a.logln("Updating the CARE engine to " + st.ExpectedVersion + ". The clinic is unavailable until this finishes; its data is kept.")
	e := a.engine()
	err := p.UpdateEngine(func() error {
		if vm.Status(context.Background()).PID == 0 {
			return nil
		}
		return e.RemoveContainers()
	})
	if err != nil {
		a.logln("The CARE engine update didn't finish; starting the clinic on the previous engine...")
		if rerr := p.OpenDocker(); rerr != nil {
			return fmt.Errorf("%w (the previous engine also couldn't start: %v)", err, rerr)
		}
		if serr := e.Start(); serr != nil {
			return fmt.Errorf("%w (the clinic also couldn't start again: %v)", err, serr)
		}
		return err
	}
	// Containers left by an engine that was already stopped when the update
	// began still carry the old logging driver; removing again is harmless.
	if err := e.RemoveContainers(); err != nil {
		return err
	}
	return e.Start()
}
