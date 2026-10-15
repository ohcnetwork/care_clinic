package main

import (
	"github.com/ohcnetwork/care_desktop/app/internal/clinic"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/tailscale"
)

type HomeAccess struct {
	tailscale.Status
	ClinicReady bool `json:"clinic_ready"`
}

func homeTarget() string { return tailscale.Target(clinic.HomePort) }

func (a *App) HomeAccessStatus() (status HomeAccess, err error) {
	defer a.logError(&err)
	if err := a.requireSetup(); err != nil {
		return status, err
	}
	e := a.engine()
	return HomeAccess{Status: tailscale.Check(e.Runner(), homeTarget()), ClinicReady: e.HomePortOpen()}, nil
}

func (a *App) EnableHomeAccess() (restart bool, err error) {
	err = a.withServerJob(func() error {
		if err := a.requireStableClinic(); err != nil {
			return err
		}
		e := a.engine()
		restart, err = enableHomeAccess(e, func() error { return tailscale.Enable(e.Runner(), homeTarget()) })
		if err == nil {
			a.logln("Home access turned on through Tailscale.")
		}
		return err
	})
	return restart, err
}

func enableHomeAccess(e *clinic.Clinic, serve func() error) (bool, error) {
	if err := serve(); err != nil {
		return false, err
	}
	changed, err := e.ApplyHomeAccessSettings()
	if err != nil {
		return false, err
	}
	return changed || !e.HomePortOpen(), nil
}

func (a *App) DisableHomeAccess() error {
	return a.withServerJob(func() error {
		if err := a.requireStableClinic(); err != nil {
			return err
		}
		if err := tailscale.Disable(a.engine().Runner(), homeTarget()); err != nil {
			return err
		}
		a.logln("Home access turned off.")
		return nil
	})
}
