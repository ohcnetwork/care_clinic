package main

import (
	"errors"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type QuitRequest struct {
	ID      uint64 `json:"id"`
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
	a.quitDialogMu.Lock()
	defer a.quitDialogMu.Unlock()
	if !a.quitUIReady {
		return QuitRequest{}, false
	}
	prompt := jobQuitPrompt(label)
	a.quitSequence++
	request := QuitRequest{ID: a.quitSequence, Title: prompt.title, Message: prompt.message}
	a.quitRequest = &request
	return request, true
}

func (a *App) takeQuitRequest(id uint64) error {
	a.quitDialogMu.Lock()
	defer a.quitDialogMu.Unlock()
	if a.quitRequest == nil || id != a.quitRequest.ID {
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
	if err := a.takeQuitRequest(id); err != nil {
		return err
	}
	if quit {
		a.logln("Quitting after confirmation while an operation may still be running.")
		a.quitConfirmed.Store(true)
		wruntime.Quit(a.ctx)
	}
	return nil
}
