package main

import "errors"

type UpdateChecks struct {
	App  bool `json:"app"`
	Care bool `json:"care"`
}

var (
	errAppUpdateChecksOff  = errors.New("CARE Clinic app update checks are turned off; an administrator can turn them back on in Advanced")
	errCareUpdateChecksOff = errors.New("CARE software update checks are turned off; an administrator can turn them back on in Advanced")
)

func (cfg Config) updateChecks() UpdateChecks {
	return UpdateChecks{App: !cfg.AppUpdateChecksOff, Care: !cfg.CareUpdateChecksOff}
}

func (a *App) appUpdateChecksOn() bool { return a.loadConfig().updateChecks().App }

func (a *App) careUpdateChecksOn() bool { return a.loadConfig().updateChecks().Care }

func (a *App) GetUpdateChecks() UpdateChecks {
	return a.loadConfig().updateChecks()
}

func (a *App) SetUpdateChecks(app, care bool, adminPassword string) error {
	return a.withJob(func() error {
		if err := a.requireAdmin(adminPassword); err != nil {
			return err
		}
		cfg := a.loadConfig()
		cfg.AppUpdateChecksOff, cfg.CareUpdateChecksOff = !app, !care
		if err := a.saveConfig(cfg); err != nil {
			return err
		}
		a.logln("Update checks: CARE Clinic app " + onOff(app) + ", CARE software " + onOff(care) + ".")
		return nil
	})
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
