package autostart

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

const (
	label   = "ohc.care-clinic"
	appName = "CARE Clinic"
	runKey  = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
)

func macPlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
}

func linuxDesktopPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "autostart", "care-clinic.desktop")
}

func Enabled() bool {
	switch runtime.GOOS {
	case "darwin":
		_, err := os.Stat(macPlistPath())
		return err == nil
	case "linux":
		_, err := os.Stat(linuxDesktopPath())
		return err == nil
	case "windows":
		return proc.Command("reg", "query", runKey, "/v", appName).Run() == nil
	}
	return false
}

func Set(on bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		if !on {
			return removeIfExists(macPlistPath())
		}
		plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>` + label + `</string>
  <key>ProgramArguments</key><array><string>` + exe + `</string><string>--autostart</string></array>
  <key>RunAtLoad</key><true/>
</dict></plist>
`
		if err := os.MkdirAll(filepath.Dir(macPlistPath()), 0o755); err != nil {
			return err
		}
		return os.WriteFile(macPlistPath(), []byte(plist), 0o644)
	case "linux":
		if !on {
			return removeIfExists(linuxDesktopPath())
		}
		desktop := "[Desktop Entry]\nType=Application\nName=" + appName +
			"\nExec=\"" + exe + "\" --autostart\nX-GNOME-Autostart-enabled=true\n"
		if err := os.MkdirAll(filepath.Dir(linuxDesktopPath()), 0o755); err != nil {
			return err
		}
		return os.WriteFile(linuxDesktopPath(), []byte(desktop), 0o644)
	case "windows":
		if !on {
			return proc.Command("reg", "delete", runKey, "/v", appName, "/f").Run()
		}
		return proc.Command("reg", "add", runKey, "/v", appName, "/t", "REG_SZ",
			"/d", `"`+exe+`" --autostart`, "/f").Run()
	}
	return nil
}

func removeIfExists(p string) error {
	if _, err := os.Stat(p); err != nil {
		return nil
	}
	return os.Remove(p)
}
