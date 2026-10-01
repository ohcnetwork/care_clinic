package main

import (
	"strings"
	"testing"
)

func TestConfirmationRequiresReadinessAndReturnsSnapshots(t *testing.T) {
	a := &App{}
	if pending, err := a.queueConfirmation("Permission", "Approve this computer"); err != nil || pending != nil {
		t.Fatalf("unregistered frontend replaced native confirmation: %+v, %v", pending, err)
	}
	if snapshot := a.SetConfirmationDialogReady(true); snapshot != nil {
		t.Fatal("registration invented a permission request")
	}
	pending, err := a.queueConfirmation("Set up care.local on this computer?", "Approve the system prompt.")
	if err != nil || pending == nil || pending.request.ID == 0 {
		t.Fatalf("could not queue confirmation: %+v, %v", pending, err)
	}
	snapshot := a.SetConfirmationDialogReady(true)
	if snapshot == nil || *snapshot != pending.request {
		t.Fatalf("registration lost the pending request: %+v", snapshot)
	}
	snapshot.ID++
	snapshot.Title = "changed"
	if current := a.SetConfirmationDialogReady(true); current == nil || *current != pending.request {
		t.Fatalf("snapshot changed the native request: %+v", current)
	}
	select {
	case answer := <-pending.answer:
		t.Fatalf("request answered without consent: %v", answer)
	default:
	}
	if other, err := a.queueConfirmation("Another request", "Wait"); err == nil || other != nil {
		t.Fatalf("a second request replaced the pending confirmation: %+v, %v", other, err)
	}
	a.SetConfirmationDialogReady(false)
	if a.awaitConfirmation(pending, nil) {
		t.Fatal("unregistering the frontend approved the request")
	}
}

func TestConfirmationAcceptsExactlyOneAnswerWithoutChangingTheJobLock(t *testing.T) {
	for _, approved := range []bool{false, true} {
		a := &App{}
		a.SetConfirmationDialogReady(true)
		pending, err := a.queueConfirmation("Permission", "Approve the system prompt.")
		if err != nil {
			t.Fatal(err)
		}
		a.jobMu.Lock()
		for _, id := range []uint64{0, pending.request.ID + 1} {
			if err := a.RespondToConfirmation(id, approved); err == nil {
				t.Fatalf("wrong request ID %d was accepted", id)
			}
		}
		if err := a.RespondToConfirmation(pending.request.ID, approved); err != nil {
			t.Fatal(err)
		}
		if got := a.awaitConfirmation(pending, nil); got != approved {
			t.Fatalf("answer changed: got %v, want %v", got, approved)
		}
		if a.jobMu.TryRLock() {
			a.jobMu.RUnlock()
			t.Fatal("responding to a permission request released the worker's job lock")
		}
		a.jobMu.Unlock()
		if err := a.RespondToConfirmation(pending.request.ID, approved); err == nil {
			t.Fatal("replayed consent was accepted")
		}
		if snapshot := a.SetConfirmationDialogReady(true); snapshot != nil {
			t.Fatal("answered request remained pending")
		}
	}
}

func TestConfirmationCancellationAndShutdownNeverApprove(t *testing.T) {
	for _, reason := range []string{"frontend closed", "runtime cancelled", "shutdown"} {
		t.Run(reason, func(t *testing.T) {
			a := &App{}
			a.SetConfirmationDialogReady(true)
			pending, err := a.queueConfirmation("Permission", "Approve the system prompt.")
			if err != nil {
				t.Fatal(err)
			}
			var done chan struct{}
			switch reason {
			case "frontend closed":
				a.SetConfirmationDialogReady(false)
			case "runtime cancelled":
				done = make(chan struct{})
				close(done)
			case "shutdown":
				a.closeConfirmations()
			}
			if a.awaitConfirmation(pending, done) {
				t.Fatal("losing the confirmation window approved the request")
			}
			if err := a.RespondToConfirmation(pending.request.ID, true); err == nil {
				t.Fatal("a late approval revived a cancelled request")
			}
			a.SetConfirmationDialogReady(true)
			next, err := a.queueConfirmation("Next permission", "Approve the system prompt.")
			if reason == "shutdown" {
				if err == nil || !strings.Contains(err.Error(), "closing") || next != nil {
					t.Fatalf("shutdown allowed another request: %+v, %v", next, err)
				}
			} else if err != nil || next == nil || next.request.ID <= pending.request.ID {
				t.Fatalf("a new request reused the cancelled ID: %+v, %v", next, err)
			}
			a.closeConfirmations()
		})
	}
}

func TestConfirmationConcurrentResponsesConsumeConsentOnce(t *testing.T) {
	a := &App{}
	a.SetConfirmationDialogReady(true)
	pending, err := a.queueConfirmation("Permission", "Approve the system prompt.")
	if err != nil {
		t.Fatal(err)
	}
	const responders = 8
	results := make(chan error, responders)
	for i := 0; i < responders; i++ {
		go func() { results <- a.RespondToConfirmation(pending.request.ID, true) }()
	}
	accepted := 0
	for i := 0; i < responders; i++ {
		if err := <-results; err == nil {
			accepted++
		}
	}
	if accepted != 1 || !a.awaitConfirmation(pending, nil) {
		t.Fatalf("permission request accepted %d answers", accepted)
	}
}
