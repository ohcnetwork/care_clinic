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
	_ = a.startAdvertise()
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

	jobPrereq = "prereq"
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
	if a.quitConfirmed.Load() {
		return false
	}
	if !a.jobMu.TryLock() {
		a.askToQuitDuringJob(a.runningJob())
		return true
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

func (a *App) askToQuitDuringJob(label string) {
	if a.ctx == nil || !a.busyShown.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.busyShown.Store(false)
		prompt := jobQuitPrompt(label)
		quit, err := a.askToProceed(prompt.title, prompt.message+"\n\nQuit anyway?", "Quit")
		if err != nil || !quit {
			return
		}
		a.logln("Quitting while " + prompt.doing + " was still running.")
		a.quitConfirmed.Store(true)
		wruntime.Quit(a.ctx)
	}()
}

type quitPrompt struct {
	doing   string
	title   string
	message string
}

const leftRunning = "Commands that already started may keep running in the background for a few minutes."

func jobQuitPrompt(label string) quitPrompt {
	switch label {
	case "setup":
		return quitPrompt{"setup", "Quit while setup is running?",
			"Setup stops where it is. The next time you open CARE Desktop you'll be back " +
				"on the setup screen, which will ask you to remove the unfinished install before " +
				"setting up again.\n\n" + leftRunning}
	case jobPrereq:
		return quitPrompt{"a requirement install", "Quit while this computer is being prepared?",
			"CARE Desktop is still installing or starting something this computer needs, " +
				"such as Rancher Desktop. That step stops where it is. The next time you open " +
				"CARE Desktop the computer check runs again and shows what is left to do.\n\n" +
				"An installer that already started may keep running in the background for a few minutes."}
	case "start", "restart":
		return quitPrompt{"starting CARE", "Quit while CARE is starting?",
			"CARE is still starting. This can take several minutes while Docker (Rancher Desktop) " +
				"comes up. Starting stops where it is; open CARE Desktop again to try once more.\n\n" + leftRunning}
	case "stop":
		return quitPrompt{"stopping CARE", "Quit while CARE is stopping?",
			"CARE is still stopping. Open CARE Desktop again to check that it stopped.\n\n" + leftRunning}
	case "restore":
		return quitPrompt{"a restore", "Quit during a restore?",
			"A restore is in progress. Quitting now can leave the clinic's data half-replaced. " +
				"If you quit, open CARE Desktop again and restore the same backup before anyone uses the clinic."}
	case "uninstall":
		return quitPrompt{"removal", "Quit while CARE is being removed?",
			"CARE is being removed from this computer. Quitting now can leave it partly removed; " +
				"open CARE Desktop again and remove it once more to finish."}
	case "app-update":
		return quitPrompt{"the CARE Desktop update", "Quit during the CARE Desktop update?",
			"The update stops and the version you have keeps working; you can update again later. " +
				"If macOS is already replacing the app, that finishes on its own."}
	case "update":
		return quitPrompt{"the CARE update", "Quit during the CARE update?",
			"CARE is being updated. Quitting can leave it partly updated; open CARE Desktop again " +
				"and run the update once more.\n\n" + leftRunning}
	case "backup-now":
		return quitPrompt{"a backup", "Quit during a backup?",
			"The backup in progress won't be finished or usable. Earlier backups are not affected.\n\n" + leftRunning}
	case "rebuild-all", "rebuild-backend", "rebuild-frontend", "apply-plugins":
		return quitPrompt{"a rebuild", "Quit during a rebuild?",
			"CARE is being rebuilt. Quitting can leave the clinic stopped; open CARE Desktop again " +
				"and run the rebuild once more.\n\n" + leftRunning}
	}
	return quitPrompt{"an operation", "Quit while CARE Desktop is working?",
		"CARE Desktop is in the middle of an operation (see the log). It stops where it is.\n\n" + leftRunning}
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
