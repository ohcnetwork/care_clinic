package main

import (
	"sync"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/health"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/keepawake"
)

const keepAwakeInterval = 30 * time.Second

type keepAwakeState struct {
	mu      sync.Mutex
	hold    *keepawake.Hold
	failed  bool
	stopped bool
}

func (a *App) KeepAwakeEnabled() bool { return a.loadConfig().KeepAwake }

func (a *App) SetKeepAwake(on bool) error {
	err := a.withJob(func() error {
		cfg := a.loadConfig()
		cfg.KeepAwake = on
		if err := a.saveConfig(cfg); err != nil {
			return err
		}
		a.logln("Keep this computer awake while the clinic runs: " + onOff(on) + ".")
		return nil
	})
	if err == nil {
		go a.syncKeepAwake()
	}
	return err
}

func (a *App) watchKeepAwake() {
	ticker := time.NewTicker(keepAwakeInterval)
	defer ticker.Stop()
	a.syncKeepAwake()
	for {
		select {
		case <-a.advStop:
			return
		case <-ticker.C:
			a.syncKeepAwake()
		}
	}
}

func (a *App) wantKeepAwake() bool {
	cfg := a.loadConfig()
	if !cfg.KeepAwake || cfg.Role != roleServer || !cfg.SetupDone || cfg.Removing {
		return false
	}
	return a.runningJob() != "" || health.Ping().Active
}

func (a *App) syncKeepAwake() {
	a.awake.mu.Lock()
	defer a.awake.mu.Unlock()
	if a.awake.stopped {
		return
	}
	want := a.wantKeepAwake()
	if a.awake.hold != nil && (!want || !a.awake.hold.Alive()) {
		a.awake.hold.Release()
		a.awake.hold = nil
		if !want {
			a.logln("This computer may sleep again.")
		}
	}
	if !want || a.awake.hold != nil {
		return
	}
	hold, err := keepawake.Start()
	if err != nil {
		if !a.awake.failed {
			a.logln("error: couldn't keep this computer awake: " + err.Error())
		}
		a.awake.failed = true
		return
	}
	a.logln("Keeping this computer awake while the clinic runs.")
	a.awake.failed = false
	a.awake.hold = hold
}

func (a *App) stopKeepAwake() {
	a.awake.mu.Lock()
	defer a.awake.mu.Unlock()
	a.awake.stopped = true
	if a.awake.hold != nil {
		a.awake.hold.Release()
		a.awake.hold = nil
	}
}
