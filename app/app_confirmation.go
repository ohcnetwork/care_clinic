package main

import (
	"errors"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type ConfirmationRequest struct {
	ID      uint64 `json:"id"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

type pendingConfirmation struct {
	request ConfirmationRequest
	answer  chan bool
}

func (a *App) SetConfirmationDialogReady(ready bool) *ConfirmationRequest {
	a.confirmationMu.Lock()
	defer a.confirmationMu.Unlock()
	a.confirmationUIReady = ready && !a.confirmationClosed
	if !a.confirmationUIReady {
		a.cancelConfirmationLocked()
	}
	if a.confirmation == nil {
		return nil
	}
	snapshot := a.confirmation.request
	return &snapshot
}

func (a *App) queueConfirmation(title, message string) (*pendingConfirmation, error) {
	a.confirmationMu.Lock()
	defer a.confirmationMu.Unlock()
	if a.confirmationClosed {
		return nil, errors.New("CARE Clinic is closing")
	}
	if !a.confirmationUIReady {
		return nil, nil
	}
	if a.confirmation != nil {
		return nil, errors.New("another permission request is still waiting for an answer")
	}
	a.confirmationSequence++
	pending := &pendingConfirmation{
		request: ConfirmationRequest{ID: a.confirmationSequence, Title: title, Message: message},
		answer:  make(chan bool, 1),
	}
	a.confirmation = pending
	a.emit("confirmation-requested", pending.request)
	return pending, nil
}

func (a *App) RespondToConfirmation(id uint64, approved bool) (err error) {
	defer a.logError(&err)
	a.confirmationMu.Lock()
	defer a.confirmationMu.Unlock()
	if a.confirmation == nil || a.confirmation.request.ID != id {
		return errors.New("this permission request is no longer active")
	}
	pending := a.confirmation
	a.confirmation = nil
	pending.answer <- approved
	return nil
}

// The worker keeps its job lock while the independent dialog channel waits.
func (a *App) awaitConfirmation(pending *pendingConfirmation, done <-chan struct{}) bool {
	select {
	case approved := <-pending.answer:
		return approved
	case <-done:
		a.confirmationMu.Lock()
		if a.confirmation == pending {
			a.cancelConfirmationLocked()
		}
		a.confirmationMu.Unlock()
		return false
	}
}

func (a *App) cancelConfirmationLocked() {
	if a.confirmation != nil {
		pending := a.confirmation
		a.confirmation = nil
		pending.answer <- false
		a.emit("confirmation-cancelled", pending.request.ID)
	}
}

func (a *App) closeConfirmations() {
	a.confirmationMu.Lock()
	defer a.confirmationMu.Unlock()
	a.confirmationClosed = true
	a.confirmationUIReady = false
	a.cancelConfirmationLocked()
}

func (a *App) confirmDialog(title, message string) bool {
	pending, err := a.queueConfirmation(title, message)
	if err != nil {
		a.logln("Permission request: " + err.Error())
		return false
	}
	if pending != nil {
		var done <-chan struct{}
		if a.ctx != nil {
			done = a.ctx.Done()
		}
		return a.awaitConfirmation(pending, done)
	}
	if a.ctx == nil {
		a.logln("Permission request: the desktop runtime is not available")
		return false
	}
	// Startup and an unavailable frontend retain an explicit native confirmation.
	sel, err := wruntime.MessageDialog(a.ctx, wruntime.MessageDialogOptions{
		Type:          wruntime.QuestionDialog,
		Title:         title,
		Message:       message,
		Buttons:       []string{"Continue", "Cancel"},
		DefaultButton: "Cancel",
		CancelButton:  "Cancel",
	})
	if err != nil {
		a.logln("Permission request: " + err.Error())
		return false
	}
	return affirmative(sel, "Continue")
}
