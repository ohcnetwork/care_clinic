package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"github.com/ohcnetwork/care_desktop/app/internal/prereq"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	quitKindJob   = "job"
	quitKindClose = "close"

	closeCancel     = "cancel"
	closeBackground = "background"
	closeQuit       = "quit"
)

type QuitRequest struct {
	ID      uint64 `json:"id"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

// Until the frontend registers its dialog, the native confirmation stays in use.
func (a *App) SetQuitDialogReady(ready bool) *QuitRequest {
	a.quitDialogMu.Lock()
	defer a.quitDialogMu.Unlock()
	a.quitUIReady = ready
	if !ready && a.quitRequest != nil {
		a.quitRequest = nil
		a.busyShown.Store(false)
	}
	if a.quitRequest == nil {
		return nil
	}
	snapshot := *a.quitRequest
	return &snapshot
}

func (a *App) queueQuitDialog(label string) (QuitRequest, bool) {
	prompt := jobQuitPrompt(label)
	return a.queueRequest(quitKindJob, prompt.title, prompt.message)
}

func (a *App) queueCloseDialog() (QuitRequest, bool) {
	prompt := closePrompt(a.loadConfig().MDNSName)
	return a.queueRequest(quitKindClose, prompt.title, prompt.message)
}

func (a *App) queueRequest(kind, title, message string) (QuitRequest, bool) {
	a.quitDialogMu.Lock()
	defer a.quitDialogMu.Unlock()
	if !a.quitUIReady {
		return QuitRequest{}, false
	}
	a.quitSequence++
	request := QuitRequest{ID: a.quitSequence, Kind: kind, Title: title, Message: message}
	a.quitRequest = &request
	return request, true
}

func (a *App) checkQuitRequest(id uint64, kind string) error {
	a.quitDialogMu.Lock()
	defer a.quitDialogMu.Unlock()
	if a.quitRequest == nil || id != a.quitRequest.ID || a.quitRequest.Kind != kind {
		return errors.New("this quit request is no longer active")
	}
	return nil
}

func (a *App) takeQuitRequest(id uint64, kind string) error {
	a.quitDialogMu.Lock()
	defer a.quitDialogMu.Unlock()
	if a.quitRequest == nil || id != a.quitRequest.ID || a.quitRequest.Kind != kind {
		return errors.New("this quit request is no longer active")
	}
	a.quitRequest = nil
	a.busyShown.Store(false)
	return nil
}

func (a *App) RespondToQuit(id uint64, quit bool) (err error) {
	defer a.logError(&err)
	if quit && a.ctx == nil {
		return errors.New("the desktop runtime is not available")
	}
	if err := a.takeQuitRequest(id, quitKindJob); err != nil {
		return err
	}
	if quit {
		a.logln("Quitting after confirmation while an operation may still be running.")
		a.quitConfirmed.Store(true)
		wruntime.Quit(a.ctx)
	}
	return nil
}

func (a *App) RespondToClose(id uint64, choice string) (err error) {
	defer a.logError(&err)
	switch choice {
	case closeCancel:
		return a.takeQuitRequest(id, quitKindClose)
	case closeBackground, closeQuit:
	default:
		return fmt.Errorf("unknown close choice %q", choice)
	}
	if a.ctx == nil {
		return errors.New("the desktop runtime is not available")
	}
	if choice == closeBackground {
		if err := a.takeQuitRequest(id, quitKindClose); err != nil {
			return err
		}
		a.sendToBackground()
		return nil
	}
	if err := a.checkQuitRequest(id, quitKindClose); err != nil {
		return err
	}
	if err := a.shutDownForQuit(); err != nil {
		return err
	}
	if err := a.takeQuitRequest(id, quitKindClose); err != nil {
		a.logln("The close request changed while the clinic was stopping; quitting as chosen.")
	}
	a.quitConfirmed.Store(true)
	wruntime.Quit(a.ctx)
	return nil
}

func (a *App) sendToBackground() {
	a.logln("CARE Clinic keeps running in the background.")
	if runtime.GOOS == "darwin" {
		wruntime.Hide(a.ctx)
		return
	}
	wruntime.WindowMinimise(a.ctx)
}

func (a *App) bringToFront() {
	if a.ctx == nil {
		return
	}
	wruntime.Show(a.ctx)
	wruntime.WindowUnminimise(a.ctx)
	wruntime.WindowShow(a.ctx)
}

func (a *App) shutDownForQuit() error {
	if !a.jobMu.TryLock() {
		return errors.New("CARE Clinic is busy with another operation; try again when it finishes")
	}
	defer a.jobMu.Unlock()
	a.activeJob.Store("quit")
	defer a.activeJob.Store("")
	if a.clinicRunning() {
		a.logln("Stopping the clinic before quitting...")
		if err := a.stopForQuit(); err != nil {
			return err
		}
	}
	a.logln("Shutting down Rancher Desktop...")
	ctx, cancel := context.WithTimeout(context.Background(), stopDeadline)
	defer cancel()
	if err := prereq.ShutdownRancher(ctx); err != nil {
		a.logln("Warning: Rancher Desktop did not confirm it shut down: " + err.Error())
	}
	a.logln("Quitting CARE Clinic after stopping the clinic.")
	return nil
}
