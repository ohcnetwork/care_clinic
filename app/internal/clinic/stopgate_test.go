package clinic

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestStopStackStopsEveryContainerAndKeepsTheData(t *testing.T) {
	f := newLifecycle(t)
	if err := f.e.StopStack(); err != nil {
		t.Fatal(err)
	}
	if running := f.running(); len(running) != 0 {
		t.Fatalf("containers are still running: %v", running)
	}
	calls := f.calls()
	if calls.exact(stopStackCall) != 0 {
		t.Fatalf("docker compose did not get the first chance to stop the stack:\n%s", strings.Join(calls, "\n"))
	}
	if calls.count("stop --time") != 0 {
		t.Fatal("containers compose had already stopped were stopped again")
	}
	assertDataKept(t, calls, false)
}

func TestStopStackSucceedsWhenThereIsNothingToStop(t *testing.T) {
	f := newLifecycle(t)
	for _, id := range []string{"c-db", "c-redis", "c-backend", "c-caddy"} {
		f.container(id, "care-clinic-"+strings.TrimPrefix(id, "c-")+"-1", "exited")
	}
	if err := f.e.StopStack(); err != nil {
		t.Fatal(err)
	}
	if calls := f.calls(); calls.count("stop --time") != 0 || calls.count("unpause") != 0 {
		t.Fatalf("an already stopped stack was stopped again:\n%s", strings.Join(calls, "\n"))
	}
}

func TestStopStackSucceedsForAProjectWithoutContainers(t *testing.T) {
	f := newLifecycle(t)
	for _, id := range []string{"c-db", "c-redis", "c-backend", "c-caddy"} {
		f.remove(id)
	}
	if err := f.e.StopStack(); err != nil {
		t.Fatal(err)
	}
	if calls := f.calls(); calls.count("inspect") != 0 {
		t.Fatalf("an empty project was inspected:\n%s", strings.Join(calls, "\n"))
	}
}

func TestStopStackStopsWhatComposeDoesNotKnowAbout(t *testing.T) {
	f := newLifecycle(t)
	f.container("c-old", "care-clinic-worker-run-1", "running")
	f.flag("orphan-c-old")
	if err := f.e.StopStack(); err != nil {
		t.Fatal(err)
	}
	if running := f.running(); len(running) != 0 {
		t.Fatalf("a container outside the compose file kept running: %v", running)
	}
	calls := f.calls()
	if calls.count("stop --time 30 c-old") != 1 {
		t.Fatalf("the leftover container was not stopped directly:\n%s", strings.Join(calls, "\n"))
	}
}

func TestStopStackFallsBackToEachContainerWhenComposeFails(t *testing.T) {
	f := newLifecycle(t)
	f.container("c-paused", "care-clinic-redis-2", "paused")
	f.fail("compose-stop")
	if err := f.e.StopStack(); err != nil {
		t.Fatal(err)
	}
	if running := f.running(); len(running) != 0 {
		t.Fatalf("containers are still running: %v", running)
	}
	calls := f.calls()
	unpause, stop := calls.first("unpause c-paused"), calls.first("stop --time 30")
	if unpause < 0 || stop < unpause {
		t.Fatalf("a paused container was not resumed before it was stopped:\n%s", strings.Join(calls, "\n"))
	}
	if strings.Contains(calls[unpause], "c-db") {
		t.Fatalf("a container that was not paused was resumed: %s", calls[unpause])
	}
	if !strings.Contains(f.logs.text(), "docker compose could not stop everything") {
		t.Fatal("the failed compose stop was not explained")
	}
	assertDataKept(t, calls, false)
}

func TestStopStackFailsWhenAContainerWillNotStop(t *testing.T) {
	f := newLifecycle(t)
	f.flag("stubborn-c-backend")
	f.fail("compose-stop")
	err := f.e.StopStack()
	if err == nil {
		t.Fatal("the gate passed while a container was still running")
	}
	for _, want := range []string{"did not stop completely", "care-clinic-backend-1", "care-clinic"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not say %q: %v", want, err)
		}
	}
	if got := f.status("c-backend"); got != "running" {
		t.Fatalf("the stubborn container is %s", got)
	}
	if f.calls().count("stop --time 30 c-backend") < 2 {
		t.Fatal("the gate did not keep trying to stop the container")
	}
}

func TestStopStackNeverTreatsADockerFailureAsAnEmptyProject(t *testing.T) {
	for _, tc := range []struct{ step, message string }{
		{"ps", "could not list CARE containers"},
		{"inspect", "could not inspect CARE containers"},
	} {
		t.Run(tc.step, func(t *testing.T) {
			f := newLifecycle(t)
			f.fail(tc.step)
			err := f.e.StopStack()
			if err == nil || !strings.Contains(err.Error(), "did not stop completely") {
				t.Fatalf("a Docker failure was not reported: %v", err)
			}
			if !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("the error does not say what failed: %v", err)
			}
		})
	}
}

func TestStopStackWaitsForContainersThatAreBeingRemoved(t *testing.T) {
	f := newLifecycle(t)
	f.container("c-gone", "care-clinic-backup-run-1", "removing")
	f.setting("settle-c-gone", "2")
	if err := f.e.StopStack(); err != nil {
		t.Fatal(err)
	}
	if got := f.status("c-gone"); got != "exited" {
		t.Fatalf("the gate returned before the container finished going away: %s", got)
	}
	if f.calls().count("stop --time") != 0 {
		t.Fatal("a container that is already being removed was stopped")
	}
}

func TestStopStackFailsWhenAContainerIsNeverRemoved(t *testing.T) {
	f := newLifecycle(t)
	f.container("c-gone", "care-clinic-backup-run-1", "removing")
	f.setting("settle-c-gone", "1000")
	if err := f.e.StopStack(); err == nil || !strings.Contains(err.Error(), "care-clinic-backup-run-1 (removing)") {
		t.Fatalf("a container stuck in removal passed the gate: %v", err)
	}
}

func TestStopStackGivesUpWhenTheJobIsCancelled(t *testing.T) {
	f := newLifecycle(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.e.ctx = ctx
	err := f.e.StopStack()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was not reported: %v", err)
	}
	if running := f.running(); len(running) == 0 {
		t.Fatal("a cancelled gate stopped the stack anyway")
	}
}
