package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/release"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/applog"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/mdns"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx       context.Context
	installFS fs.FS
	pins      *release.Pins
	log       *applog.Logger

	cfgMu      sync.RWMutex
	cfg        Config
	configFile string

	jobMu     sync.RWMutex
	closing   bool
	activeJob atomic.Value
	busyShown atomic.Bool

	quitConfirmed atomic.Bool

	advMu    sync.Mutex
	adv      *mdns.Advertiser
	advError error
	advStop  chan struct{}

	store storageWatch

	osUninstall  bool
	removeTarget string
}

func NewApp(installFS fs.FS, log *applog.Logger) (*App, error) {
	proc.FixPath()
	env, err := fs.ReadFile(installFS, "install/"+release.EnvFile)
	if err != nil {
		return nil, fmt.Errorf("this build is missing its embedded %s: %w", release.EnvFile, err)
	}
	pins, err := release.Load(env)
	if err != nil {
		return nil, err
	}
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	cfg, err := loadConfig(path)
	if err != nil {
		return nil, err
	}
	a := &App{installFS: installFS, pins: pins, log: log, cfg: cfg, configFile: path}
	if err := a.inferInstalledRole(); err != nil {
		return nil, err
	}
	if a.loadConfig().Role == roleServer {
		if err := a.saveConfig(a.loadConfig()); err != nil {
			return nil, err
		}
		if _, err := a.engine().Backups().PendingRestore(); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *App) logln(msg string) {
	a.log.Write(msg)
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "care-log", msg)
	}
}

func (a *App) emit(event string, data ...any) {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, event, data...)
	}
}

// Failures are logged and conflicts are shown here; background callers retry.
func (a *App) startAdvertise() error {
	a.advMu.Lock()
	defer a.advMu.Unlock()
	select {
	case <-a.advStop:
		return nil
	default:
	}
	if a.adv != nil {
		return nil
	}
	cfg := a.loadConfig()
	name := cfg.MDNSName
	if cfg.Role != roleServer || name == "" || cfg.Removing || (!cfg.SetupDone && cfg.AdminPwHash == "") {
		a.advError = nil
		return nil
	}
	adv, err := mdns.Advertise(name, a.logln)
	if err != nil {
		a.reportAdvertiseError(err)
		return err
	}
	a.adv = adv
	a.advError = nil
	return nil
}

func (a *App) restartAdvertise() error {
	a.advMu.Lock()
	a.adv.Stop()
	a.adv = nil
	a.advMu.Unlock()
	return a.startAdvertise()
}

// Caller holds advMu. Report a continuing conflict once, not every watcher tick.
func (a *App) reportAdvertiseError(err error) {
	if a.advError != nil && a.advError.Error() == err.Error() {
		return
	}
	var previous, conflict *mdns.ConflictError
	continuingConflict := errors.As(a.advError, &previous) &&
		errors.As(err, &conflict) && previous.Host == conflict.Host
	a.advError = err
	a.logln("mDNS: " + err.Error())
	if errors.As(err, &conflict) && !continuingConflict {
		go a.notifyActionFailed("network-name", err.Error()+
			"\n\nNetwork advertising is paused. Ask your clinic administrator to resolve the duplicate addresses. No clinic data has been changed.")
	}
}

func (a *App) advRunning() bool {
	a.advMu.Lock()
	defer a.advMu.Unlock()
	return a.adv != nil
}

func (a *App) watchAdvertise() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	misses := 0
	for {
		select {
		case <-a.advStop:
			return
		case <-t.C:
			a.advMu.Lock()
			adv := a.adv
			a.advMu.Unlock()
			if adv == nil {
				_ = a.startAdvertise()
				continue
			}
			changed, err := adv.IPsChanged()
			if err != nil {
				a.logln("mDNS: couldn't inspect the LAN (" + err.Error() + ")")
			}
			if changed || err != nil {
				misses = 0
				_ = a.restartAdvertise()
				continue
			}
			if err := mdns.CheckAvailable(adv.Name()); err != nil {
				var conflict *mdns.ConflictError
				inUse := errors.As(err, &conflict)
				a.advMu.Lock()
				if a.adv == adv {
					if inUse {
						adv.Stop()
						a.adv = nil
					}
					a.reportAdvertiseError(err)
				}
				a.advMu.Unlock()
				if inUse {
					misses = 0
					continue
				}
			}
			if err := adv.Resolves(); err == nil {
				misses = 0
				continue
			} else {
				a.logln("mDNS: hostname probe failed (" + err.Error() + ")")
			}
			misses++
			if misses >= 2 {
				misses = 0
				a.logln("mDNS: " + adv.Name() + ".local stopped resolving - re-advertising.")
				_ = a.restartAdvertise()
			}
		}
	}
}
