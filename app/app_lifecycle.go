package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/prereq"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"

	"github.com/wailsapp/wails/v2/pkg/options"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.fitWindowToScreen()
	if a.osUninstall {
		return
	}
	a.refreshInstallDir()
	a.advStop = make(chan struct{})
	a.startAdvertise()
	go a.watchAdvertise()
	if a.loadConfig().Role != roleServer {
		return
	}
	go func() {
		a.log.Writef("docker: %s", prereq.DockerCheck(a.engine().Runner()).Message)
		prereq.EnsureRancherSettings()
	}()
	go a.watchForCareUpdates()
	go a.watchStorage()
}

const screenMargin = 80

func (a *App) fitWindowToScreen() {
	if a.ctx == nil || a.ctx.Value("frontend") == nil {
		return
	}
	screens, err := wruntime.ScreenGetAll(a.ctx)
	if err != nil {
		return
	}
	screen, ok := currentScreen(screens)
	if !ok {
		return
	}
	width := max(min(windowWidth, screen.Size.Width-screenMargin), windowMinWidth)
	height := max(min(windowHeight, screen.Size.Height-screenMargin), windowMinHeight)
	if width == windowWidth && height == windowHeight {
		return
	}
	wruntime.WindowSetSize(a.ctx, width, height)
	wruntime.WindowCenter(a.ctx)
}

func currentScreen(screens []wruntime.Screen) (wruntime.Screen, bool) {
	var fallback wruntime.Screen
	found := false
	for _, s := range screens {
		if s.Size.Width <= 0 || s.Size.Height <= 0 {
			continue
		}
		if s.IsCurrent {
			return s, true
		}
		if !found || s.IsPrimary {
			fallback, found = s, true
		}
	}
	return fallback, found
}

func (a *App) refreshInstallDir() {
	updated := false
	if err := a.withJob(func() error {
		cfg := a.loadConfig()
		if cfg.Role != roleServer || !cfg.SetupDone || cfg.Removing {
			return nil
		}
		pending, err := a.engine().Backups().PendingRestore()
		if err != nil {
			return err
		}
		if pending {
			a.logln("An unfinished restore was found; its installed configuration was left unchanged for recovery.")
			return nil
		}
		if err := a.syncInstallKit(); err != nil {
			return err
		}
		updated = true
		return nil
	}); err != nil {
		a.logln("error: couldn't update the installed configuration (" + err.Error() + ")")
		return
	}
	if updated {
		a.logln("Install files are up to date with this version.")
	}
}

func (a *App) syncInstallKit() error {
	if _, err := a.ensureInstallDir(); err != nil {
		return err
	}
	return a.engine().ApplyDomain()
}

func (a *App) shutdown(context.Context) {
	if a.advStop != nil {
		close(a.advStop)
	}
	a.advMu.Lock()
	a.adv.Stop()
	a.adv = nil
	a.advMu.Unlock()
}

const (
	singleInstanceID = "ohc.care-desktop"

	quitPromptTimeout = 30 * time.Second

	stopDeadline = 90 * time.Second
)

type quitChoice int

const (
	quitLeaveClinicRunning quitChoice = iota
	quitStayOpen
	quitStopClinic
)

func (a *App) onSecondInstance(options.SecondInstanceData) {
	if a.ctx == nil {
		return
	}
	wruntime.WindowUnminimise(a.ctx)
	wruntime.Show(a.ctx)
}

func (a *App) beforeClose(context.Context) (prevent bool) {
	if !a.jobMu.TryLock() {
		return !a.quitDuringJob(a.runningJob())
	}
	defer a.jobMu.Unlock()
	if a.closing {
		return false
	}
	if a.osUninstall || a.loadConfig().Role != roleServer {
		a.closing = true
		return false
	}
	answer := make(chan quitChoice, 1)
	go func() { answer <- a.askBeforeQuit() }()

	select {
	case sel := <-answer:
		switch sel {
		case quitStayOpen:
			return true
		case quitStopClinic:
			if err := a.stopForQuit(); err != nil {
				a.logln("error: " + err.Error())
				a.notifyActionFailed("stop", err.Error())
				return true
			}
		}
		a.closing = true
		return false
	case <-time.After(quitPromptTimeout):
		a.closing = true
		return false
	}
}

func (a *App) runningJob() string {
	label, _ := a.activeJob.Load().(string)
	return label
}

func (a *App) quitDuringJob(label string) bool {
	if label != "setup" {
		a.logln("An operation is still running. Wait for it to finish before closing CARE Desktop.")
		if a.ctx != nil && a.busyShown.CompareAndSwap(false, true) {
			go func() {
				defer a.busyShown.Store(false)
				_, _ = wruntime.MessageDialog(a.ctx, wruntime.MessageDialogOptions{
					Type:    wruntime.InfoDialog,
					Title:   "CARE Desktop is still working",
					Message: busyQuitMessage(label),
					Buttons: []string{"OK"},
				})
			}()
		}
		return false
	}
	if a.ctx == nil || !a.busyShown.CompareAndSwap(false, true) {
		return false
	}
	answer := make(chan bool, 1)
	go func() {
		defer a.busyShown.Store(false)
		quit, err := a.askToProceed("Quit while setup is running?",
			"Setup stops where it is. The next time you open CARE Desktop you'll be back "+
				"on the setup screen, which will ask you to remove the unfinished install before "+
				"setting up again.\n\n"+
				"Docker steps that already started may keep running in the background for a few minutes.\n\n"+
				"Quit anyway?", "Quit")
		answer <- err == nil && quit
	}()
	select {
	case quit := <-answer:
		if quit {
			a.logln("Quitting during setup; setup will start over next time.")
		}
		return quit
	case <-time.After(quitPromptTimeout):
		return false
	}
}

func busyQuitMessage(label string) string {
	switch label {
	case "restore":
		return "A restore is in progress. Quitting now could leave the clinic's data half-replaced.\n\n" +
			"Wait for it to finish, then quit."
	case "uninstall":
		return "CARE is being removed from this computer. Quitting now could leave it partly removed.\n\n" +
			"Wait for it to finish, then quit."
	case "app-update":
		return "A CARE Desktop update is being prepared. Wait for it to finish, then quit."
	case "update":
		return "CARE is being updated. Wait for the update to finish, then quit."
	}
	return "CARE Desktop is in the middle of an operation (see the log). Wait for it to finish, then quit."
}

func (a *App) askBeforeQuit() quitChoice {
	if !a.clinicRunning() {
		return quitLeaveClinicRunning
	}
	name := a.loadConfig().MDNSName
	quit, err := a.askToProceed("Quit CARE Desktop?",
		"The clinic keeps running, but "+name+" will stop working for other devices "+
			"on the clinic's WiFi until this app is open again.\n\nQuit anyway?", "Yes")
	if err != nil {
		return quitLeaveClinicRunning
	}
	if !quit {
		return quitStayOpen
	}
	stop, err := a.askToProceed("Shut the clinic down as well?",
		"Shutting down stops the clinic on this computer completely, so nobody can "+
			"use it until it is started again.\n\nChoose No to leave it running.", "Yes")
	if err != nil || !stop {
		return quitLeaveClinicRunning
	}
	return quitStopClinic
}

func (a *App) clinicRunning() bool {
	cfg := a.loadConfig()
	if cfg.Role != roleServer || !cfg.SetupDone || cfg.Removing {
		return false
	}
	if _, err := os.Stat(filepath.Join(a.installDir(), "docker-compose.yml")); err != nil {
		return false
	}
	out, err := a.engine().Status()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasSuffix(strings.TrimSpace(line), " running") {
			return true
		}
	}
	return false
}

func (a *App) stopForQuit() error {
	if err := a.requireServer(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), stopDeadline)
	defer cancel()
	run := a.engine().Runner()
	cmd := proc.CommandContext(ctx, "docker", "compose", "stop")
	cmd.Dir, cmd.Env = run.Dir, run.Env
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("Docker did not finish stopping; CARE Desktop was kept open: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
