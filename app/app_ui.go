package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/autostart"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) OpenURL(url string) { wruntime.BrowserOpenURL(a.ctx, url) }

func affirmative(sel, yes string) bool { return sel == yes || sel == "Yes" }

func (a *App) askToProceed(title, message, yes string) (bool, error) {
	sel, err := wruntime.MessageDialog(a.ctx, wruntime.MessageDialogOptions{
		Type:          wruntime.QuestionDialog,
		Title:         title,
		Message:       message,
		Buttons:       []string{yes, "No"},
		DefaultButton: "No",
		CancelButton:  "No",
	})
	if err != nil {
		return false, err
	}
	return affirmative(sel, yes), nil
}

// alertDialog is for failures the interface cannot show, because the window is
// already closing. Everything else uses the care-error event.
func (a *App) alertDialog(title, message string) {
	if a.ctx == nil {
		return
	}
	_, _ = wruntime.MessageDialog(a.ctx, wruntime.MessageDialogOptions{
		Type:    wruntime.ErrorDialog,
		Title:   title,
		Message: message,
		Buttons: []string{"OK"},
	})
}

func (a *App) ChooseFolder(title string) (dir string, err error) {
	defer a.logError(&err)
	opts := wruntime.OpenDialogOptions{Title: title}
	if runtime.GOOS == "windows" {
		desktop, err := proc.DesktopDir()
		if err != nil {
			return "", fmt.Errorf("couldn't locate your Desktop: %w", err)
		}
		opts.DefaultDirectory = desktop
	} else if home, err := os.UserHomeDir(); err == nil {
		opts.DefaultDirectory = home
	}
	return wruntime.OpenDirectoryDialog(a.ctx, opts)
}

func recoverySaveDirectory() (string, error) {
	if runtime.GOOS != "windows" {
		return "", nil
	}
	return proc.DesktopDir()
}

func (a *App) OpenSetupRecoveryFolder(codes bool) error {
	return a.withReadJob(func() error {
		if runtime.GOOS != "windows" {
			return errors.New("opening the recovery folder is available only on Windows")
		}
		if err := a.requireRecoverySetup(); err != nil {
			return err
		}
		cfg := a.loadConfig()
		path := cfg.BackupRecoveryPath
		if codes {
			path = cfg.AdminRecoveryPath
		}
		if path == "" {
			return errors.New("save the recovery file before opening its folder")
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("couldn't find the saved recovery file: %w", err)
		}
		cmd := proc.Command("explorer.exe", "/select,"+path)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("couldn't open the recovery folder: %w", err)
		}
		go func() { _ = cmd.Wait() }()
		return nil
	})
}

func openDocument(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = proc.Command("open", path)
	case "windows":
		cmd = proc.Command("rundll32.exe", "url.dll,FileProtocolHandler", path)
	default:
		cmd = proc.Command("xdg-open", path)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("couldn't open the saved document: %w", err)
	}
	return nil
}

func (a *App) WasAutostartLaunched() bool {
	return slices.Contains(os.Args, "--autostart")
}

func (a *App) LogPath() string { return a.log.Path() }

func (a *App) OpenLogFolder() (err error) {
	defer a.logError(&err)
	path := a.log.Path()
	if path == "" {
		return errors.New("this run isn't writing a log file - the log folder couldn't be opened for writing")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = proc.Command("open", "-R", path)
	case "windows":
		cmd = proc.Command("explorer.exe", "/select,"+path)
	default:
		cmd = proc.Command("xdg-open", filepath.Dir(path))
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("couldn't open the log folder: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func (a *App) AutostartEnabled() bool { return autostart.Enabled() }

func (a *App) SetAutostart(on bool) error {
	return a.withJob(func() error { return autostart.Set(on) })
}
