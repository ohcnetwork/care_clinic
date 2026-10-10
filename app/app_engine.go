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
	// Logs is "connecting", "collecting" or "interrupted" when running; an
	// empty value while running means an older helper that cannot report it.
	Logs string `json:"logs"`
}

func engineState(installed, expected string, toolsOK bool, s vm.HostStatus) EngineState {
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
	return st
}

func (a *App) EngineStatus() EngineState {
	if !vm.Supported() {
		return EngineState{}
	}
	installed, expected, toolsOK := a.provisioner().EngineVersions()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return engineState(installed, expected, toolsOK, vm.Status(ctx))
}

// updateEngine runs inside a clinic job, automatically on the first launch
// after an app update or from the panel's retry button. The new engine is
// downloaded and verified first; only then are the containers removed (volumes
// kept) so they are recreated on the new engine with its logging settings.
// The clinic is started again afterwards, on the old engine if the update failed.
func (a *App) updateEngine() error {
	if !vm.Supported() {
		return errors.New("this computer runs CARE on Docker directly; there is no CARE engine to update")
	}
	st := a.EngineStatus()
	if !st.UpdateNeeded {
		a.logln("The CARE engine is already up to date.")
		return nil
	}
	a.logln("Updating the CARE engine to " + st.ExpectedVersion + ". The clinic is unavailable until this finishes; its data is kept.")
	e := a.engine()
	p := a.provisioner()
	err := p.UpdateEngine(func() error {
		if vm.Status(context.Background()).PID == 0 {
			return nil
		}
		return e.RemoveContainers()
	})
	if err != nil {
		a.logln("The CARE engine update didn't finish; starting the clinic on the current engine...")
		if rerr := p.OpenDocker(); rerr != nil {
			return fmt.Errorf("%w (the current engine also couldn't start: %v)", err, rerr)
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
