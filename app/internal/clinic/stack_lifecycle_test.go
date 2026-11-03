package clinic

import (
	"strings"
	"testing"
)

func TestStartNeverInstallsAnOfferedUpdate(t *testing.T) {
	f := newLifecycle(t)
	f.encryptBackups()
	f.offer(newCommit, newCommit)

	if err := f.e.Start(); err != nil {
		t.Fatal(err)
	}

	calls := f.calls()
	if calls.count("build ") != 0 || calls.count("image tag") != 0 || calls.count("compose run") != 0 {
		t.Fatalf("starting CARE built, promoted or backed up an offered update:\n%s", strings.Join(calls, "\n"))
	}
	lock := f.lock()
	requireChannel(t, "backend", lock.Backend, oldCommit, newCommit)
	requireChannel(t, "frontend", lock.Frontend, oldCommit, newCommit)
	if running := f.running(); len(running) != 4 {
		t.Fatalf("CARE is not running after Start: %v", running)
	}
	assertDataKept(t, calls, false)
}

func TestStartStopsLeftoverServicesBeforeItRefreshesOrBuildsAnything(t *testing.T) {
	f := newLifecycle(t)
	f.container("c-left", "care-clinic-backup-run-1", "running")
	f.flag("orphan-c-left")
	refreshed := 0
	f.e.Refresh = func() error {
		refreshed++
		if running := f.running(); len(running) != 0 {
			t.Errorf("installed files were refreshed while CARE was running: %v", running)
		}
		if calls := f.calls(); calls.count("compose up") != 0 || calls.count("build ") != 0 {
			t.Errorf("CARE was built or started before the refresh:\n%s", strings.Join(calls, "\n"))
		}
		return nil
	}

	if err := f.e.Start(); err != nil {
		t.Fatal(err)
	}

	if refreshed != 1 {
		t.Fatalf("the refresh ran %d times", refreshed)
	}
	if got := f.status("c-left"); got != "exited" {
		t.Fatalf("a leftover CARE container is %s", got)
	}
	if calls := f.calls(); calls.exact(stopStackCall) != 0 {
		t.Fatalf("CARE was not stopped first:\n%s", strings.Join(calls, "\n"))
	}
}

func TestStartTouchesNothingWhenTheStackCannotBeStopped(t *testing.T) {
	f := newLifecycle(t)
	f.offer(newCommit, newCommit)
	f.flag("stubborn-c-backend")
	f.fail("compose-stop")
	f.e.Refresh = func() error {
		t.Error("files were refreshed while a CARE container was still running")
		return nil
	}

	err := f.e.Start()
	if err == nil || !strings.Contains(err.Error(), "did not stop completely") {
		t.Fatalf("Start went ahead with a container still running: %v", err)
	}
	calls := f.calls()
	for _, prefix := range []string{"build ", "compose up", "compose down", "image tag"} {
		if n := calls.count(prefix); n != 0 {
			t.Fatalf("%d %q command(s) ran against a stack that was still running:\n%s", n, prefix, strings.Join(calls, "\n"))
		}
	}
}

func TestStartStopsAndStartsAnAlreadyStoppedStack(t *testing.T) {
	f := newLifecycle(t)
	for _, service := range []string{"db", "redis", "backend", "caddy"} {
		f.container("c-"+service, "care-clinic-"+service+"-1", "exited")
	}
	if err := f.e.Start(); err != nil {
		t.Fatal(err)
	}
	if running := f.running(); len(running) != 4 {
		t.Fatalf("CARE is not running: %v", running)
	}
}

func TestStopKeepsEverythingButTheRunningContainers(t *testing.T) {
	f := newLifecycle(t)
	if err := f.e.Stop(); err != nil {
		t.Fatal(err)
	}
	if running := f.running(); len(running) != 0 {
		t.Fatalf("containers are still running: %v", running)
	}
	calls := f.calls()
	if calls.count("compose up") != 0 || calls.count("build ") != 0 {
		t.Fatalf("Stop started or built something:\n%s", strings.Join(calls, "\n"))
	}
	assertDataKept(t, calls, false)
	if err := f.e.Stop(); err != nil {
		t.Fatalf("stopping a stopped CARE failed: %v", err)
	}
}

func TestRestartStopsTheStackAndStartsItAgain(t *testing.T) {
	f := newLifecycle(t)
	f.offer(newCommit, newCommit)
	if err := f.e.Restart(); err != nil {
		t.Fatal(err)
	}
	calls := f.calls()
	requireInOrder(t, calls,
		[2]string{"stop", "=" + stopStackCall},
		[2]string{"backend start", "=" + startBackendCall},
		[2]string{"full start", "=" + startAllCall},
	)
	if calls.count("build ") != 0 || calls.count("image tag") != 0 {
		t.Fatalf("a restart installed an offered update:\n%s", strings.Join(calls, "\n"))
	}
	if running := f.running(); len(running) != 4 {
		t.Fatalf("CARE is not running: %v", running)
	}
}

func TestRebuildStopsCAREBeforeItRefreshesAndBuilds(t *testing.T) {
	f := newLifecycle(t)
	f.e.Refresh = func() error {
		if running := f.running(); len(running) != 0 {
			t.Errorf("installed files were refreshed while CARE was running: %v", running)
		}
		return nil
	}

	if err := f.e.RebuildBackend(); err != nil {
		t.Fatal(err)
	}

	calls := f.calls()
	requireInOrder(t, calls,
		[2]string{"stop", "=" + stopStackCall},
		[2]string{"teardown", "compose down"},
		[2]string{"build", "build -f "},
		[2]string{"backend start", "=" + startBackendCall},
		[2]string{"migration", migrateCall},
		[2]string{"full start", "=" + startAllCall},
	)
	if calls.exact(stopStackCall) != 0 {
		t.Fatalf("CARE was not stopped first:\n%s", strings.Join(calls, "\n"))
	}
	if running := f.running(); len(running) != 4 {
		t.Fatalf("CARE is not running after the rebuild: %v", running)
	}
	assertDataKept(t, calls, true)
}

func TestRebuildChangesNothingWhenCAREWillNotStop(t *testing.T) {
	f := newLifecycle(t)
	f.flag("stubborn-c-backend")
	f.fail("compose-stop")

	if err := f.e.RebuildAll(); err == nil || !strings.Contains(err.Error(), "did not stop completely") {
		t.Fatalf("the rebuild went ahead with CARE running: %v", err)
	}
	calls := f.calls()
	for _, prefix := range []string{"compose down", "build ", "compose up"} {
		if n := calls.count(prefix); n != 0 {
			t.Fatalf("%d %q command(s) ran against a stack that was still running:\n%s", n, prefix, strings.Join(calls, "\n"))
		}
	}
}

func TestRebuildStartsThePreviousImagesWhenTheBuildFails(t *testing.T) {
	f := newLifecycle(t)
	f.fail("build")

	err := f.e.RebuildFrontend()
	if err == nil {
		t.Fatal("a failed build was reported as a success")
	}
	if strings.Contains(err.Error(), "could not be started again") {
		t.Fatalf("CARE was not started again: %v", err)
	}
	if running := f.running(); len(running) != 4 {
		t.Fatalf("CARE is not running on its previous images: %v", running)
	}
}
