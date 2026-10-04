package applog

import (
	"os"
	"path/filepath"
	"runtime"
)

const logStem = "care-log"

const logName = logStem + ".log"

// appFolder matches the name every folder this app owns uses, on every OS. It
// mirrors appDirName in package main; the two cannot share a constant because
// internal/ must not depend on the app package.
const appFolder = "care-clinic"

func DefaultLogDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Logs", appFolder)
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, appFolder, "logs")
	default:
		base := os.Getenv("XDG_STATE_HOME")
		if base == "" {
			base = filepath.Join(home, ".local", "state")
		}
		return filepath.Join(base, appFolder)
	}
}
