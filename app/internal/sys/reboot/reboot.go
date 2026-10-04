package reboot

import (
	"fmt"
	"runtime"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

type Plan struct {
	Needed bool   `json:"needed"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Label  string `json:"label"`
}

var windowsRebootKeys = []string{
	`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending`,
	`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\RebootRequired`,
}

func Check() Plan {
	if !windowsPending() {
		return Plan{}
	}
	return Plan{
		Needed: true,
		Title:  "Restart to finish setting up Docker",
		Detail: "Windows needs to restart before Docker can run - it has to turn on WSL 2, " +
			"which only takes effect after a restart. CARE Clinic will open again by itself " +
			"and pick up where you left off.",
		Label: "Restart now",
	}
}

func windowsPending() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	for _, key := range windowsRebootKeys {
		if proc.Command("reg", "query", key).Run() == nil {
			return true
		}
	}
	return false
}

func Now() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("restarting isn't supported on %s", runtime.GOOS)
	}
	return proc.Command("shutdown", "/r", "/t", "5", "/c",
		"CARE Clinic is restarting this computer to finish setting up Docker.").Run()
}
