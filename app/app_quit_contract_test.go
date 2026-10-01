package main

import (
	"strings"
	"testing"
)

func TestQuitDialogRequiresReadinessAndReturnsImmutableSnapshots(t *testing.T) {
	a := &App{}
	if request, ready := a.queueQuitDialog("setup"); ready || request != (QuitRequest{}) {
		t.Fatalf("unregistered frontend replaced the native fallback: %+v, %v", request, ready)
	}
	if snapshot := a.SetQuitDialogReady(true); snapshot != nil {
		t.Fatalf("registration invented a pending request: %+v", snapshot)
	}
	request, ready := a.queueQuitDialog("restore")
	if !ready || request.ID == 0 {
		t.Fatalf("registered frontend did not receive a request: %+v, %v", request, ready)
	}
	prompt := jobQuitPrompt("restore")
	if request.Title != prompt.title || request.Message != prompt.message {
		t.Fatalf("queued request changed the native warning: %+v", request)
	}
	want := request
	request.ID++
	request.Title, request.Message = "changed", "changed"
	a.busyShown.Store(true)
	snapshot := a.SetQuitDialogReady(true)
	if snapshot == nil || *snapshot != want {
		t.Fatalf("changing the queued value changed the pending request: %+v", snapshot)
	}
	snapshot.ID++
	snapshot.Title, snapshot.Message = "changed again", "changed again"
	current := a.SetQuitDialogReady(true)
	if current == nil || *current != want || !a.busyShown.Load() {
		t.Fatalf("changing a readiness snapshot changed the pending request: %+v", current)
	}
}

func TestQuitDialogCancellationRejectsWrongAndReplayedIDsWithoutUnlockingJob(t *testing.T) {
	a := &App{}
	a.SetQuitDialogReady(true)
	request, ready := a.queueQuitDialog("setup")
	if !ready {
		t.Fatal("registered frontend did not receive a request")
	}
	a.busyShown.Store(true)
	a.activeJob.Store("setup")
	a.jobMu.Lock()
	owned := true
	defer func() {
		if owned {
			a.jobMu.Unlock()
		}
	}()
	checkWorker := func() {
		t.Helper()
		if a.jobMu.TryRLock() {
			a.jobMu.RUnlock()
			owned = false
			t.Fatal("frontend response unlocked the worker's exclusive job")
		}
		if a.closing || a.quitConfirmed.Load() || a.runningJob() != "setup" {
			t.Fatal("cancelling or rejecting a quit response changed worker/closing state")
		}
	}
	for _, id := range []uint64{0, request.ID + 1} {
		if err := a.RespondToQuit(id, false); err == nil || !strings.Contains(err.Error(), "no longer active") {
			t.Fatalf("wrong request %d was accepted: %v", id, err)
		}
		snapshot := a.SetQuitDialogReady(true)
		if snapshot == nil || *snapshot != request || !a.busyShown.Load() {
			t.Fatalf("wrong response consumed the active request: %+v", snapshot)
		}
		checkWorker()
	}
	if err := a.RespondToQuit(request.ID, false); err != nil {
		t.Fatalf("active request could not be cancelled: %v", err)
	}
	if a.SetQuitDialogReady(true) != nil || a.busyShown.Load() {
		t.Fatal("cancel did not consume the request and clear the displayed-prompt flag")
	}
	checkWorker()
	if err := a.RespondToQuit(request.ID, false); err == nil {
		t.Fatal("replayed cancellation was accepted")
	}
	checkWorker()
}

func TestQuitDialogInvalidationAndReplacementKeepRequestIDsMonotonic(t *testing.T) {
	a := &App{}
	a.SetQuitDialogReady(true)
	first, _ := a.queueQuitDialog("setup")
	a.busyShown.Store(true)
	if snapshot := a.SetQuitDialogReady(false); snapshot != nil || a.busyShown.Load() {
		t.Fatal("unregistering the frontend retained its pending request")
	}
	if request, ready := a.queueQuitDialog("start"); ready || request != (QuitRequest{}) {
		t.Fatalf("unregistered frontend received another request: %+v, %v", request, ready)
	}
	if err := a.RespondToQuit(first.ID, false); err == nil {
		t.Fatal("a response from the unregistered frontend was accepted")
	}
	if snapshot := a.SetQuitDialogReady(true); snapshot != nil {
		t.Fatalf("registration resurrected an invalidated request: %+v", snapshot)
	}
	second, ready := a.queueQuitDialog("start")
	if !ready || second.ID <= first.ID {
		t.Fatalf("new registration reused a stale request ID: first=%+v, second=%+v", first, second)
	}
	third, ready := a.queueQuitDialog("backup-now")
	if !ready || third.ID <= second.ID {
		t.Fatalf("replacement did not receive a fresh ID: second=%+v, third=%+v", second, third)
	}
	a.busyShown.Store(true)
	for _, stale := range []uint64{first.ID, second.ID} {
		if err := a.takeQuitRequest(stale); err == nil {
			t.Fatalf("superseded request %d was accepted", stale)
		}
		snapshot := a.SetQuitDialogReady(true)
		if snapshot == nil || *snapshot != third || !a.busyShown.Load() {
			t.Fatalf("superseded response changed the active request: %+v", snapshot)
		}
	}
	if err := a.takeQuitRequest(third.ID); err != nil {
		t.Fatalf("current request could not be consumed: %v", err)
	}
	if a.SetQuitDialogReady(true) != nil || a.busyShown.Load() {
		t.Fatal("consuming the request retained a pending prompt")
	}
	if err := a.takeQuitRequest(third.ID); err == nil {
		t.Fatal("consumed request was replayed")
	}
}

func TestQuitDialogConcurrentCancellationConsumesExactlyOnce(t *testing.T) {
	a := &App{}
	a.SetQuitDialogReady(true)
	request, _ := a.queueQuitDialog("setup")
	a.busyShown.Store(true)
	const responders = 8
	results := make(chan error, responders)
	for i := 0; i < responders; i++ {
		go func() {
			results <- a.RespondToQuit(request.ID, false)
		}()
	}
	accepted := 0
	for i := 0; i < responders; i++ {
		if err := <-results; err == nil {
			accepted++
		} else if !strings.Contains(err.Error(), "no longer active") {
			t.Fatalf("concurrent response failed for an unrelated reason: %v", err)
		}
	}
	if accepted != 1 {
		t.Fatalf("request was consumed %d times, want exactly once", accepted)
	}
	if a.SetQuitDialogReady(true) != nil || a.busyShown.Load() || a.closing || a.quitConfirmed.Load() {
		t.Fatal("concurrent cancellation retained the prompt or initiated closing")
	}
}
