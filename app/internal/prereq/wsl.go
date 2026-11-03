package prereq

import (
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/reboot"
)

const wslInstallPage = "https://aka.ms/wslinstall"

const vmPlatformFeature = "VirtualMachinePlatform"

type WSLStatus struct {
	Applicable bool   `json:"applicable"`
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
	How        string `json:"how"`
	Fixable    bool   `json:"fixable"`
}

var vmPlatformOn atomic.Bool

func vmPlatformEnabled() bool {
	if vmPlatformOn.Load() {
		return true
	}
	out, err := proc.Command("powershell", "-NoProfile", "-Command",
		`(Get-CimInstance Win32_OptionalFeature -Filter "Name='`+vmPlatformFeature+`'").InstallState`).Output()
	if err != nil {
		return true
	}
	if strings.TrimSpace(string(out)) != "1" {
		return false
	}
	vmPlatformOn.Store(true)
	return true
}

func wslAnswers() bool {
	return proc.Command("wsl", "--status").Run() == nil
}

func wslReady() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	return wslAnswers() && vmPlatformEnabled()
}

func WSLCheck() WSLStatus {
	if runtime.GOOS != "windows" {
		return WSLStatus{Applicable: false, OK: true}
	}
	if wslReady() {
		return WSLStatus{Applicable: true, OK: true, Message: "WSL 2 is on"}
	}
	status := WSLStatus{
		Applicable: true, Fixable: true,
		Message: "WSL 2 is off",
		How: "Docker runs inside WSL 2, so it has to be on before Rancher Desktop will install. " +
			"Click Turn on WSL 2; Windows will ask for permission.",
	}
	if wslAnswers() {
		status.Message = "WSL 2 is installed, but Windows' Virtual Machine Platform is off"
		status.How = "Docker's virtual machine cannot be reached without it, so Docker would " +
			"install and then never answer. Click Turn on WSL 2 to switch the feature back on."
	}
	if reboot.Check().Needed {
		status.Message += ", and Windows is waiting for a restart"
		status.How = "Restart this computer and run the check again. If WSL 2 is still off after " +
			"the restart, click Turn on WSL 2."
	}
	return status
}

func (pr *Provisioner) InstallWSL() (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("WSL is only used on Windows, not %s", runtime.GOOS)
	}
	pr.logln("Turning on Windows Subsystem for Linux, which Docker needs...")
	if err := pr.runElevated("dism.exe", "/online", "/enable-feature",
		"/featurename:"+vmPlatformFeature, "/all", "/norestart"); err != nil {
		return "", fmt.Errorf("could not turn on the Virtual Machine Platform that WSL 2 needs; "+
			"turn WSL 2 on from %s and try again: %w", wslInstallPage, err)
	}
	// The wsl.exe that ships with older Windows 10 builds rejects --no-distribution
	// and exits -1; updating first replaces it with one that understands the flag.
	if err := pr.runElevated("wsl", "--update"); err != nil {
		pr.logln("Warning: could not update WSL before turning it on: " + err.Error())
	}
	if err := pr.runElevated("wsl", "--install", "--no-distribution"); err != nil {
		return "", fmt.Errorf("could not turn on WSL 2; turn it on from %s and try again: %w",
			wslInstallPage, err)
	}
	vmPlatformOn.Store(false)
	if wslReady() && !reboot.Check().Needed {
		pr.logln("WSL 2 is on.")
		return "", nil
	}
	pr.logln("WSL 2 is installed but needs a restart.")
	return "WSL 2 has been turned on.\n\nWindows has to restart before Docker can be installed. " +
		"Restart this computer, then choose Check again.", nil
}
