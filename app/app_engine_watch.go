package main

import (
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/prereq"
)

// The engine watch brings Rancher Desktop back when it is left open over a
// stopped virtual machine (a WSL update from Windows Update does this) or has
// quit. Nothing else would: Rancher does not restart its VM, and the panel's
// checks only report. The clinic's containers restart with Docker on their
// own (restart: unless-stopped), so reviving Rancher is the whole recovery;
// a clinic the operator stopped stays stopped.
const (
	engineWatchInterval = 30 * time.Second
	// engineDownGrace keeps one bad reading, such as the moment after waking
	// from sleep, from restarting anything.
	engineDownGrace = time.Minute
	// engineRetryAfter spaces out attempts when a restart does not help.
	engineRetryAfter = 10 * time.Minute
)

type engineWatch struct {
	downSince   time.Time
	lastAttempt time.Time
}

// observe records one check and reports whether to restart Rancher now.
// broken means Docker is not answering and Rancher is stuck or not running;
// Rancher that is merely slow to answer is left alone.
func (w *engineWatch) observe(now time.Time, broken bool) bool {
	if !broken {
		w.downSince = time.Time{}
		return false
	}
	if w.downSince.IsZero() {
		w.downSince = now
	}
	if now.Sub(w.downSince) < engineDownGrace {
		return false
	}
	if !w.lastAttempt.IsZero() && now.Sub(w.lastAttempt) < engineRetryAfter {
		return false
	}
	w.lastAttempt = now
	return true
}

func (a *App) watchEngine() {
	var w engineWatch
	ticker := time.NewTicker(engineWatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-a.advStop:
			return
		case <-ticker.C:
			if w.observe(time.Now(), a.engineBroken()) {
				a.recoverEngine()
			}
		}
	}
}

func (a *App) engineBroken() bool {
	cfg := a.loadConfig()
	if cfg.Role != roleServer || !cfg.SetupDone || cfg.Removing || a.closing {
		return false
	}
	if a.runningJob() != "" {
		// A start, update or restore owns Docker for now and reports its own failures.
		return false
	}
	if prereq.DockerCheck(a.engine().Runner()).OK {
		return false
	}
	return prereq.RancherDown()
}

func (a *App) recoverEngine() {
	if err := a.lockJob(); err != nil {
		return
	}
	defer a.jobMu.Unlock()
	a.activeJob.Store(jobPrereq)
	defer a.activeJob.Store("")
	a.logln("Docker has not answered for over a minute; bringing Rancher Desktop back automatically...")
	// OpenDocker starts a Rancher that quit and fully restarts a stuck one.
	if err := a.provisioner().OpenDocker(); err != nil {
		a.logln("error: automatic Rancher Desktop restart failed: " + err.Error())
		return
	}
	a.logln("Rancher Desktop is back; the clinic's containers restart on their own.")
}
