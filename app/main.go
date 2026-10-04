package main

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/applog"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/appremoval"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/logger"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

const (
	windowWidth     = 1100
	windowHeight    = 700
	windowMinWidth  = 720
	windowMinHeight = 560
)

var appLog *applog.Logger

//go:embed all:frontend/dist
var assets embed.FS

//go:embed all:install
var installFS embed.FS

func main() {
	osUninstall := slices.Contains(os.Args, uninstallFlag)
	uninstallCheck := slices.Contains(os.Args, uninstallCheckFlag)
	appLog = applog.Open()
	defer appLog.Close()
	appLog.OnFatal = func(msg string) { fatal(errors.New(msg)) }

	if osUninstall || uninstallCheck {
		if code, done := removalPreflight(); done {
			appLog.Writef("uninstall: stopped before checking the setup (exit %d)", code)
			exit(code)
		}
	}

	app, err := NewApp(installFS, appLog)
	if err != nil {
		if uninstallCheck {
			appLog.Writef("uninstall: %s", err)
			exit(exitSetUp)
		}
		fatal(err)
	}
	if osUninstall || uninstallCheck {
		code := app.removalExitCode()
		if code == exitRemovable || uninstallCheck {
			appLog.Writef("uninstall: setup check finished (exit %d)", code)
			exit(code)
		}
		app.osUninstall = true
	}

	appLog.Header(app.pins.AppVersion, app.installDir(), app.loadConfig().MDNSName)
	for _, line := range app.pins.Summary() {
		appLog.Write(line)
	}

	err = wails.Run(&options.App{
		Title:            "CARE Clinic",
		Width:            windowWidth,
		Height:           windowHeight,
		MinWidth:         windowMinWidth,
		MinHeight:        windowMinHeight,
		BackgroundColour: &options.RGBA{R: 249, G: 250, B: 251, A: 255},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		Logger:             appLog,
		LogLevel:           logger.INFO,
		LogLevelProduction: logger.INFO,
		HideWindowOnClose:  runtime.GOOS == "darwin",
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               singleInstanceID,
			OnSecondInstanceLaunch: app.onSecondInstance,
		},
		OnStartup:     app.startup,
		OnBeforeClose: app.beforeClose,
		OnShutdown:    app.shutdown,
		Bind:          []interface{}{app},
	})
	if err != nil {
		fatal(err)
	}
	if app.osUninstall {
		exit(app.removalExitCode())
	}
	if app.removeTarget != "" {
		if err := appremoval.Remove(app.removeTarget); err != nil {
			alert("CARE Clinic was not removed", fmt.Errorf("the clinic setup was removed, but the app could not be: %w", err))
		}
	}
}

func exit(code int) {
	appLog.Close()
	os.Exit(code)
}

func fatal(err error) {
	const title = "CARE Clinic can't start"
	appLog.Writef("FATAL %s: %s", title, err)
	show(title, err)
	os.Exit(1)
}

func alert(title string, err error) {
	appLog.Writef("%s: %s", title, err)
	show(title, err)
}

func show(title string, err error) {
	appLog.Close()
	fmt.Fprintln(os.Stderr, title+": "+err.Error())
	switch runtime.GOOS {
	case "darwin":
		_ = proc.Command("osascript", "-e", "display alert "+elevate.OSAQuote(title)+
			" message "+elevate.OSAQuote(err.Error())+" as critical").Run()
	case "windows":
		_ = proc.Command("powershell", "-NoProfile", "-Command",
			"Add-Type -AssemblyName PresentationFramework; [System.Windows.MessageBox]::Show("+
				elevate.PSQuote(err.Error())+","+elevate.PSQuote(title)+")").Run()
	}
}
