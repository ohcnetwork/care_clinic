package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"strings"
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

	// Owned by jobMu; retains the original setup password in memory only.
	setupAttempt *setupAttempt

	// The clinic root FindClinic validated, held only in memory until Connect
	// pins it, so looking for a clinic writes nothing.
	pendingMu   sync.Mutex
	pendingURL  string
	pendingRoot string

	quitConfirmed atomic.Bool
	quitDialogMu  sync.Mutex
	quitUIReady   bool
	quitSequence  uint64
	quitRequest   *QuitRequest

	confirmationMu       sync.Mutex
	confirmationUIReady  bool
	confirmationClosed   bool
	confirmationSequence uint64
	confirmation         *pendingConfirmation

	advMu     sync.Mutex
	adv       nameAdvertiser
	advSince  time.Time
	advError  error
	advStop   chan struct{}
	nameCheck func(string) error

	store storageWatch

	awake keepAwakeState

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

// logError writes the error a bound method is about to hand back to the
// interface into the log file, naming the method, so a friendly message on
// screen always has a technical counterpart the operator can send to support.
// It is deferred with a pointer to the named return value:
//
//	func (a *App) Something() (err error) {
//		defer a.logError(&err)
//
// The job helpers do this for everything that runs through them, so only bound
// methods that do their own locking need the line - and nothing needs it twice.
func (a *App) logError(err *error) {
	if err != nil {
		*err = a.logged(*err)
	}
}

// loggedError marks an error that is already in the log file, so a bound method
// and the helper it ran through do not each write the same line.
type loggedError struct{ error }

func (a *App) logged(err error) error {
	var already loggedError
	if err == nil || errors.As(err, &already) {
		return err
	}
	a.logln(boundMethod() + ": " + err.Error())
	return loggedError{err}
}

// boundMethod walks out to the exported App method the interface actually
// called, so the log names ConnectClient rather than the helper that happened
// to produce the error.
func boundMethod() string {
	pcs := make([]uintptr, 12)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs)])
	for {
		frame, more := frames.Next()
		if _, method, ok := strings.Cut(frame.Function, "(*App)."); ok {
			name, _, _ := strings.Cut(method, ".")
			if name != "" && name[0] >= 'A' && name[0] <= 'Z' {
				return name
			}
		}
		if !more {
			return "CARE Clinic"
		}
	}
}

// reportError puts a failure on the interface. It replaces the native error
// dialogs that used to interrupt whatever the operator was doing; the desktop
// decides how to show it. The caller has already written the line to the log.
func (a *App) reportError(label, detail string) {
	a.emit("care-error", label, detail)
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
	a.advSince = time.Now()
	a.advError = nil
	return nil
}

func (a *App) restartAdvertise() error {
	a.advMu.Lock()
	if a.adv != nil {
		a.adv.Stop()
	}
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
		a.reportError(failureTitle("network-name"), err.Error()+
			"\n\nNetwork advertising is paused. Ask your clinic administrator to resolve the duplicate addresses. No clinic data has been changed.")
	}
}

func (a *App) advRunning() bool {
	a.advMu.Lock()
	defer a.advMu.Unlock()
	return a.adv != nil
}

const (
	advertiseInterval = 30 * time.Second
	advertiseRecheck  = 5 * time.Second
	advertiseSettle   = 12 * time.Second
)

type nameAdvertiser interface {
	Name() string
	IPsChanged() (bool, error)
	CheckAvailable() error
	Resolves() error
	Stop()
}

type advertiseWatch struct {
	sightings int
	misses    int
}

func (w *advertiseWatch) reset() {
	w.sightings, w.misses = 0, 0
}

func (w *advertiseWatch) sighted() bool {
	w.sightings++
	return w.sightings >= 2
}

func (a *App) watchAdvertise() {
	var w advertiseWatch
	timer := time.NewTimer(advertiseInterval)
	defer timer.Stop()
	for {
		select {
		case <-a.advStop:
			return
		case <-timer.C:
			timer.Reset(a.checkAdvertise(&w, time.Now()))
		}
	}
}

func (a *App) checkAdvertise(w *advertiseWatch, now time.Time) time.Duration {
	a.advMu.Lock()
	adv, since := a.adv, a.advSince
	a.advMu.Unlock()
	if adv == nil {
		w.reset()
		_ = a.startAdvertise()
		return advertiseInterval
	}
	if a.lanChanged(adv) {
		w.reset()
		_ = a.restartAdvertise()
		return advertiseInterval
	}
	if !now.Before(since.Add(advertiseSettle)) {
		err := adv.CheckAvailable()
		var conflict *mdns.ConflictError
		inUse := errors.As(err, &conflict)
		if mdns.Unsettled(err) || (inUse && a.lanChanged(adv)) {
			a.logln("mDNS: the network changed during the name check (" + err.Error() + ") - re-advertising.")
			w.reset()
			_ = a.restartAdvertise()
			return advertiseInterval
		}
		if inUse && !w.sighted() {
			a.logln("mDNS: " + err.Error() + " Checking again before reporting it.")
			return advertiseRecheck
		}
		if err != nil {
			a.advMu.Lock()
			if a.adv == adv {
				if inUse {
					adv.Stop()
					a.adv = nil
				}
				a.reportAdvertiseError(err)
			}
			a.advMu.Unlock()
		}
		if inUse {
			w.reset()
			return advertiseInterval
		}
	}
	w.sightings = 0
	if err := adv.Resolves(); err == nil {
		w.misses = 0
		return advertiseInterval
	} else {
		a.logln("mDNS: hostname probe failed (" + err.Error() + ")")
	}
	w.misses++
	if w.misses >= 2 {
		w.reset()
		a.logln("mDNS: " + adv.Name() + ".local stopped resolving - re-advertising.")
		_ = a.restartAdvertise()
	}
	return advertiseInterval
}

func (a *App) lanChanged(adv nameAdvertiser) bool {
	changed, err := adv.IPsChanged()
	if err != nil {
		a.logln("mDNS: couldn't inspect the LAN (" + err.Error() + ")")
	}
	return changed || err != nil
}

func (a *App) checkName(name string) error {
	if a.nameCheck != nil {
		return a.nameCheck(name)
	}
	return mdns.CheckAvailable(name)
}
