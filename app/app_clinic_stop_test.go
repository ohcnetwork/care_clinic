package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/clinic"
)

// fakeLifecycleDocker is a container engine that records every call in
// $CARE_FAKE_DIR/calls and answers only what the stop gate and the checks before
// it ask. Files in that folder steer it:
//
//	hold, release  "compose stop" waits until release exists
//	live           container ids that "docker ps" lists and "docker inspect" calls running
//	stubborn       "compose stop" fails
//
// It has no images, so starting CARE on it always fails early, and never
// reaches Docker's real engine.
const fakeLifecycleDocker = `#!/bin/sh
dir="$CARE_FAKE_DIR"
printf '%s\n' "$*" >> "$dir/calls"
case "$1" in
  version) echo linux/27.0.0 ;;
  ps) [ -f "$dir/live" ] && cat "$dir/live" ;;
  inspect)
    shift 3
    for id in "$@"; do echo "$id /$id running"; done ;;
  image) exit 1 ;;
  compose)
    if [ "$2" = stop ]; then
      while [ -f "$dir/hold" ] && [ ! -f "$dir/release" ]; do sleep 0.02; done
      [ -f "$dir/stubborn" ] && exit 1
    fi ;;
esac
exit 0
`

type fakeEngine struct{ dir string }

func (f fakeEngine) write(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f fakeEngine) calls() []string {
	data, err := os.ReadFile(filepath.Join(f.dir, "calls"))
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func (f fakeEngine) count(call string) int {
	n := 0
	for _, got := range f.calls() {
		if got == call {
			n++
		}
	}
	return n
}

// lifecycleApp is an installed clinic server whose docker and git are fakes.
func lifecycleApp(t *testing.T) (*App, fakeEngine) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX command fixtures")
	}
	a := settingsApp(t)
	a.cfg.Role = roleServer
	bin, state := t.TempDir(), t.TempDir()
	for name, script := range map[string]string{"docker": fakeLifecycleDocker, "git": "#!/bin/sh\nexit 1\n"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CARE_FAKE_DIR", state)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	return a, fakeEngine{state}
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitForJob(t *testing.T, a *App) {
	t.Helper()
	eventually(t, "the job to finish", func() bool {
		if !a.jobMu.TryLock() {
			return false
		}
		a.jobMu.Unlock()
		return true
	})
}

func TestApplyEnvChangesRejectsBadRequestsBeforeAnythingRuns(t *testing.T) {
	good := []clinic.EnvChange{{Name: "backend", Content: "DB_BACKUP_RETENTION_PERIOD=0\n"}}
	for _, tc := range []struct{ state, want string }{
		{"wrong password", "admin password does not match"},
		{"invalid syntax", "invalid syntax"},
		{"unknown file", "unknown env file"},
		{"given twice", "given twice"},
		{"nothing to apply", "no settings to apply"},
		{"not installed", "admin password does not match"},
		{"client", "clinic server"},
		{"removing", "cleanup is incomplete"},
		{"restore pending", ""},
		{"plugin rollback", ""},
		{"busy", "still running"},
		{"closing", "closing"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			a, engine := lifecycleApp(t)
			if err := a.checkEnvChanges(good, settingsPassword); err != nil {
				t.Fatalf("the request is not valid to begin with: %v", err)
			}
			changes, password := good, settingsPassword
			switch tc.state {
			case "wrong password":
				password = "wrong"
			case "invalid syntax":
				changes = []clinic.EnvChange{{Name: "frontend", Content: "REACT_TITLE=\"unbalanced\n"}}
			case "unknown file":
				changes = []clinic.EnvChange{{Name: "caddy", Content: "A=1\n"}}
			case "given twice":
				changes = append(append([]clinic.EnvChange(nil), good...), good...)
			case "nothing to apply":
				changes = nil
			case "not installed":
				a.cfg.SetupDone = false
			case "client":
				a.cfg = Config{Role: roleClient, ClientURL: "https://clinic.local/"}
			case "removing":
				a.cfg.Removing = true
			case "restore pending":
				if err := os.WriteFile(filepath.Join(a.installDir(), "restore-state.json"), []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "plugin rollback":
				if err := os.WriteFile(filepath.Join(a.installDir(), "plugin-recovery.json"), []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "busy":
				a.jobMu.Lock()
				defer a.jobMu.Unlock()
			case "closing":
				a.closing = true
			}
			backend, frontend := backendEnv(t, a), readInstalled(t, a, "frontend.env")

			err := a.ApplyEnvChanges(changes, password)

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the request was accepted, or refused for another reason: %v", err)
			}
			if got := a.runningJob(); got != "" {
				t.Fatalf("a refused request started the %q job", got)
			}
			if tc.state != "busy" {
				onboardingJobUnlocked(t, a)
			}
			if backendEnv(t, a) != backend || readInstalled(t, a, "frontend.env") != frontend {
				t.Fatal("a refused request changed a settings file")
			}
			if calls := engine.calls(); len(calls) != 0 {
				t.Fatalf("a refused request reached Docker: %v", calls)
			}
		})
	}
}

func readInstalled(t *testing.T, a *App, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(a.installDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestApplyEnvChangesStopsCareBeforeChangingAnyFile(t *testing.T) {
	a, engine := lifecycleApp(t)
	before := backendEnv(t, a)
	engine.write(t, "hold", "")
	t.Cleanup(func() { engine.write(t, "release", "") })

	err := a.ApplyEnvChanges([]clinic.EnvChange{{Name: "backend", Content: "DB_BACKUP_RETENTION_PERIOD=0\n"}}, settingsPassword)
	if err != nil {
		t.Fatalf("a valid request was refused: %v", err)
	}
	eventually(t, "CARE to be asked to stop", func() bool { return engine.count("compose stop") == 1 })
	if got := a.runningJob(); got != "apply-settings" {
		t.Fatalf("the job is called %q", got)
	}
	if a.jobMu.TryLock() {
		a.jobMu.Unlock()
		t.Fatal("the job does not hold the job lock while CARE stops")
	}
	if got := backendEnv(t, a); got != before {
		t.Fatalf("settings changed before CARE had stopped: %q", got)
	}

	engine.write(t, "release", "")
	waitForJob(t, a)

	// CARE cannot start on this fake engine, so the old settings must be back.
	if got := backendEnv(t, a); got != before {
		t.Fatalf("CARE did not start on the new settings but they were kept: %q", got)
	}
	if n := engine.count("compose stop"); n != 2 {
		t.Fatalf("CARE was stopped %d times, want once to apply and once to go back", n)
	}
	if n := engine.count("compose up -d --wait --wait-timeout 300 db redis backend"); n != 0 {
		t.Fatalf("CARE was started %d times on an engine that cannot build it", n)
	}
	if checking.Load() {
		t.Fatal("the job kept update checks off")
	}
}

func TestApplyEnvChangesChangesNothingWhileAnUpdateCheckRuns(t *testing.T) {
	a, engine := lifecycleApp(t)
	before := backendEnv(t, a)
	wait, poll := updateCheckWait, updateCheckPoll
	updateCheckWait, updateCheckPoll = 30*time.Millisecond, time.Millisecond
	checking.Store(true)
	t.Cleanup(func() {
		updateCheckWait, updateCheckPoll = wait, poll
		checking.Store(false)
	})

	err := a.ApplyEnvChanges([]clinic.EnvChange{{Name: "backend", Content: "DB_BACKUP_RETENTION_PERIOD=0\n"}}, settingsPassword)
	if err != nil {
		t.Fatalf("a valid request was refused: %v", err)
	}
	waitForJob(t, a)

	if calls := engine.calls(); len(calls) != 0 {
		t.Fatalf("CARE was touched while an update check was running: %v", calls)
	}
	if backendEnv(t, a) != before {
		t.Fatal("settings changed while an update check was running")
	}
	if !checking.Load() {
		t.Fatal("the job cleared a check it did not start")
	}
}

func TestHoldUpdateChecks(t *testing.T) {
	wait, poll := updateCheckWait, updateCheckPoll
	t.Cleanup(func() {
		updateCheckWait, updateCheckPoll = wait, poll
		checking.Store(false)
	})
	updateCheckPoll = time.Millisecond

	t.Run("waits for a check that is running", func(t *testing.T) {
		updateCheckWait = 10 * time.Second
		checking.Store(true)
		go func() {
			time.Sleep(40 * time.Millisecond)
			checking.Store(false)
		}()
		release, err := holdUpdateChecks()
		if err != nil {
			t.Fatalf("a check that finished was not waited for: %v", err)
		}
		if !checking.Load() {
			t.Fatal("update checks are not held")
		}
		release()
		if checking.Load() {
			t.Fatal("update checks stayed off after they were released")
		}
	})

	t.Run("gives up on a check that never ends", func(t *testing.T) {
		updateCheckWait = 30 * time.Millisecond
		checking.Store(true)
		release, err := holdUpdateChecks()
		if err == nil || release != nil || !strings.Contains(err.Error(), "update check is still running") {
			t.Fatalf("got %v, %v", release != nil, err)
		}
		if !checking.Load() {
			t.Fatal("giving up cleared a check that is still running")
		}
		checking.Store(false)
	})
}

func TestOnlyActionsThatChangeWhatRunsHoldUpdateChecks(t *testing.T) {
	for action, want := range map[string]bool{
		"start": true, "restart": true, "update": true, "apply-plugins": true,
		"stop": false, "rebuild-all": false, "rebuild-backend": false, "rebuild-frontend": false,
		"backup-now": false, "free-space": false,
	} {
		if got := holdsUpdateChecks(action); got != want {
			t.Errorf("holdsUpdateChecks(%q) = %v, want %v", action, got, want)
		}
	}
}

func TestEngineForRefreshesTheInstallKitWhereCareIsStartedOrRebuilt(t *testing.T) {
	a := settingsApp(t)
	for action, want := range map[string]bool{
		"start": true, "restart": true, "rebuild-all": true,
		"stop": false, "rebuild-backend": false, "rebuild-frontend": false, "apply-plugins": false,
		"update": false, "backup-now": false, "free-space": false,
	} {
		if got := a.engineFor(action).Refresh != nil; got != want {
			t.Errorf("%s refreshes the install kit = %v, want %v", action, got, want)
		}
	}
}

func TestTakeLaunchStartOffersOneStartPerRunOfAnInstalledServer(t *testing.T) {
	t.Run("installed server", func(t *testing.T) {
		a := settingsApp(t)
		a.cfg.Role = roleServer
		if !a.TakeLaunchStart() {
			t.Fatal("an installed clinic server was not offered a start")
		}
		if a.TakeLaunchStart() {
			t.Fatal("the launch start was offered twice")
		}
	})
	for name, change := range map[string]func(a *App){
		"unchosen":   func(a *App) { a.cfg = Config{} },
		"client":     func(a *App) { a.cfg = Config{Role: roleClient, ClientURL: "https://clinic.local/"} },
		"not set up": func(a *App) { a.cfg = Config{Role: roleServer} },
		"removing":   func(a *App) { a.cfg.Role, a.cfg.Removing = roleServer, true },
		"uninstall":  func(a *App) { a.cfg.Role, a.osUninstall = roleServer, true },
	} {
		t.Run(name, func(t *testing.T) {
			a := settingsApp(t)
			change(a)
			if a.TakeLaunchStart() {
				t.Fatal("a launch start was offered where there is no clinic to start")
			}
			// Declining must not use up the one start the run has.
			a.cfg = Config{Role: roleServer, SetupDone: true}
			a.osUninstall = false
			if !a.TakeLaunchStart() {
				t.Fatal("declining used up the launch start")
			}
		})
	}
}

func TestStopClinicForAppUpdateHoldsUpdateChecksUntilReleased(t *testing.T) {
	a, engine := lifecycleApp(t)

	release, stopped, err := a.stopClinicForAppUpdate()
	if err != nil || !stopped {
		t.Fatalf("CARE was not stopped: %v, %v", stopped, err)
	}
	if !checking.Load() {
		t.Fatal("update checks run while CARE is stopped for an update")
	}
	if n := engine.count("compose stop"); n != 1 {
		t.Fatalf("CARE was stopped %d times", n)
	}
	release()
	if checking.Load() {
		t.Fatal("update checks stayed off after the update")
	}
}

// The fake engine cannot start CARE, which makes the restart after a failed
// download fail too. Both problems must reach the person who chose to update.
func TestAppUpdateStopsCareBeforeDownloadingAndStartsItAgainWhenTheDownloadFails(t *testing.T) {
	a, engine := lifecycleApp(t)
	var afterStop atomic.Int32
	asset, want, requests := damagedInstaller(t, func() {
		if engine.count("compose stop") > 0 {
			afterStop.Add(1)
		}
	})

	err := a.installRelease(asset, want, "1.2.3")

	if err == nil || !strings.Contains(err.Error(), "didn't download properly") {
		t.Fatalf("the failed download was not reported: %v", err)
	}
	if !strings.Contains(err.Error(), "CARE could not be started again") {
		t.Fatalf("the failed restart was not reported: %v", err)
	}
	if requests.Load() == 0 || afterStop.Load() != requests.Load() {
		t.Fatalf("%d of %d downloads began after CARE had stopped", afterStop.Load(), requests.Load())
	}
	if n := engine.count("compose stop"); n != 2 {
		t.Fatalf("CARE was stopped %d times, want once for the update and once to start it again", n)
	}
	if checking.Load() {
		t.Fatal("the update kept update checks off")
	}
}

func TestAppUpdateDoesNotDownloadWhenCareCannotBeStopped(t *testing.T) {
	a, engine := lifecycleApp(t)
	engine.write(t, "live", "abc123\n")
	engine.write(t, "stubborn", "")
	asset, want, requests := damagedInstaller(t, nil)

	err := a.installRelease(asset, want, "1.2.3")

	if err == nil || !strings.Contains(err.Error(), "CARE did not stop completely") {
		t.Fatalf("the update went ahead, or failed for another reason: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatal("the installer was downloaded while CARE was still running")
	}
	if engine.count("stop --time 30 abc123") == 0 {
		t.Fatalf("the container that would not stop was not stopped directly: %v", engine.calls())
	}
	if checking.Load() {
		t.Fatal("the update kept update checks off")
	}
}

func TestAppUpdateLeavesCareAloneWhenThereIsNoClinicToStop(t *testing.T) {
	for name, cfg := range map[string]Config{
		"unchosen":   {},
		"client":     {Role: roleClient, ClientURL: "https://clinic.local/"},
		"not set up": {Role: roleServer},
		"removing":   {Role: roleServer, SetupDone: true, Removing: true},
	} {
		t.Run(name, func(t *testing.T) {
			a, engine := lifecycleApp(t)
			a.cfg = cfg
			asset, want, requests := damagedInstaller(t, nil)

			err := a.installRelease(asset, want, "1.2.3")

			if err == nil || !strings.Contains(err.Error(), "didn't download properly") ||
				strings.Contains(err.Error(), "started again") {
				t.Fatalf("got %v", err)
			}
			if requests.Load() == 0 {
				t.Fatal("the update did not go ahead")
			}
			if calls := engine.calls(); len(calls) != 0 {
				t.Fatalf("a computer with no clinic to stop used Docker: %v", calls)
			}
			if checking.Load() {
				t.Fatal("the update kept update checks off")
			}
		})
	}
}

// damagedInstaller serves an installer whose checksum is not the published one,
// so the download fails after it is made.
func damagedInstaller(t *testing.T, onRequest func()) (asset ghAsset, want string, requests *atomic.Int32) {
	t.Helper()
	requests = new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if onRequest != nil {
			onRequest()
		}
		_, _ = w.Write([]byte("damaged"))
	}))
	t.Cleanup(server.Close)
	sum := sha256.Sum256([]byte("the published installer"))
	return ghAsset{Name: "CARE-Clinic-1.2.3-setup.exe", URL: server.URL}, hex.EncodeToString(sum[:]), requests
}
