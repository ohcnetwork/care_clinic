package main

import (
	"context"
	"errors"
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

// updateEngine runs inside a clinic job. Containers are removed (volumes kept)
// before the engine is replaced so that they are recreated on the new engine
// with its logging settings rather than restarted with the old ones.
func (a *App) updateEngine() error {
	if !vm.Supported() {
		return errors.New("this computer runs CARE on Docker directly; there is no CARE engine to update")
	}
	st := a.EngineStatus()
	if !st.UpdateNeeded {
		a.logln("The CARE engine is already up to date.")
		return nil
	}
	if !a.confirmDialog("Update the CARE engine?", "CARE Clinic will stop the clinic, install CARE engine "+
		st.ExpectedVersion+", and start the clinic again. This takes a few minutes and needs an internet connection. "+
		"Patient records, uploaded files and backups are kept.") {
		a.logln("CARE engine update cancelled.")
		return nil
	}
	e := a.engine()
	start := a.clinicRunning()
	if st.Running {
		if err := e.RemoveContainers(); err != nil {
			return err
		}
	}
	if err := a.provisioner().UpdateEngine(); err != nil {
		return err
	}
	if !st.Running {
		// The engine was stopped, so its containers could not be removed first.
		if err := e.RemoveContainers(); err != nil {
			return err
		}
	}
	if !start {
		a.logln("CARE engine " + st.ExpectedVersion + " is running. Start the clinic when you're ready.")
		return nil
	}
	return e.Start()
}
