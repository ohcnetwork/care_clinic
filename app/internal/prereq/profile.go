package prereq

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

// Rancher Desktop reads a deployment profile before its first run. A profile
// that sets anything at all also tells it to skip the welcome dialog, so CARE
// writes one before Rancher Desktop is ever started - not only when CARE is the
// one installing it.
const rancherProfileVersion = 18

const (
	rancherMemoryGB = 3
	rancherSwapGB   = 4
)

const rancherProfilePlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>version</key><integer>%d</integer>
	<key>application</key>
	<dict>
		<key>adminAccess</key><true/>
		<key>autoStart</key><false/>
		<key>startInBackground</key><true/>
		<key>pathManagementStrategy</key><string>rcfiles</string>
		<key>hideNotificationIcon</key><true/>
		<key>updater</key>
		<dict><key>enabled</key><false/></dict>
		<key>telemetry</key>
		<dict><key>enabled</key><false/></dict>
		<key>window</key>
		<dict><key>quitOnClose</key><false/></dict>
		<key>extensions</key>
		<dict>
			<key>allowed</key>
			<dict><key>enabled</key><true/><key>list</key><array/></dict>
		</dict>
	</dict>
	<key>diagnostics</key>
	<dict>
		<key>connectivity</key>
		<dict><key>interval</key><integer>0</integer></dict>
	</dict>
	<key>containerEngine</key>
	<dict><key>name</key><string>moby</string></dict>
	<key>kubernetes</key>
	<dict><key>enabled</key><false/></dict>
	<key>virtualMachine</key>
	<dict><key>memoryInGB</key><integer>%d</integer></dict>
</dict>
</plist>
`

// Rancher Desktop reads its registry profile from "Policies\Rancher Desktop"
// and then from "Rancher Desktop\Profile", and the later one wins. Windows
// keeps HKCU\Software\Policies read-only for anyone who isn't an administrator,
// so CARE writes the second, which needs no elevation.
const rancherProfileKey = `HKCU\Software\Rancher Desktop\Profile\Defaults`

func writeRancherProfile() error {
	var swapErr error
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dir := filepath.Join(home, "Library", "Preferences")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		body := fmt.Sprintf(rancherProfilePlist, rancherProfileVersion, rancherMemoryGB)
		path := filepath.Join(dir, "io.rancherdesktop.profile.defaults.plist")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return err
		}
		override := filepath.Join(home, "Library", "Application Support", "rancher-desktop", "lima", "_config", "override.yaml")
		swapErr = writeRancherSwapOverride(override)
	case "windows":
		if err := writeRancherProfileWindows(); err != nil {
			return err
		}
	default:
		return nil
	}
	applyRancherProfileNow()
	return swapErr
}

const rancherSwapScript = `#!/bin/sh
swapfile=/mnt/data/care-clinic.swap
for _ in $(seq 30); do mountpoint -q /mnt/data && break; sleep 1; done
mountpoint -q /mnt/data || exit 0
if [ ! -f "$swapfile" ]; then
  if ! { fallocate -l %dG "$swapfile" && chmod 600 "$swapfile" && mkswap "$swapfile" >/dev/null; }; then
    rm -f "$swapfile"
    exit 0
  fi
fi
grep -q "^$swapfile " /proc/swaps || swapon "$swapfile" || true
exit 0
`

func rancherSwapOverride() string {
	script := fmt.Sprintf(rancherSwapScript, rancherSwapGB)
	return "provision:\n- mode: system\n  script: |\n    " +
		strings.ReplaceAll(strings.TrimSuffix(script, "\n"), "\n", "\n    ") + "\n"
}

func writeRancherSwapOverride(path string) error {
	if _, err := os.Lstat(path); err == nil || !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(rancherSwapOverride()), 0o644)
}

func writeRancherProfileWindows() error {
	if rancherProfileWritten() {
		return nil
	}
	_ = proc.Command("reg", "delete", rancherProfileKey+`\application`,
		"/v", rancherAdminAccessValue, "/f").Run()
	args := [][]string{
		{rancherProfileKey, "/v", "version", "/t", "REG_DWORD", "/d", fmt.Sprint(rancherProfileVersion)},
		{rancherProfileKey + `\application`, "/v", "autoStart", "/t", "REG_DWORD", "/d", "0"},
		{rancherProfileKey + `\application`, "/v", "startInBackground", "/t", "REG_DWORD", "/d", "1"},
		{rancherProfileKey + `\application`, "/v", "hideNotificationIcon", "/t", "REG_DWORD", "/d", "1"},
		{rancherProfileKey + `\application\updater`, "/v", "enabled", "/t", "REG_DWORD", "/d", "0"},
		{rancherProfileKey + `\application\telemetry`, "/v", "enabled", "/t", "REG_DWORD", "/d", "0"},
		{rancherProfileKey + `\application\window`, "/v", "quitOnClose", "/t", "REG_DWORD", "/d", "0"},
		{rancherProfileKey + `\application\extensions\allowed`, "/v", "enabled", "/t", "REG_DWORD", "/d", "1"},
		{rancherProfileKey + `\diagnostics\connectivity`, "/v", "interval", "/t", "REG_DWORD", "/d", "0"},
		{rancherProfileKey + `\containerEngine`, "/v", "name", "/t", "REG_SZ", "/d", "moby"},
		{rancherProfileKey + `\kubernetes`, "/v", "enabled", "/t", "REG_DWORD", "/d", "0"},
	}
	for _, a := range args {
		if err := proc.Command("reg", append([]string{"add"}, append(a, "/f")...)...).Run(); err != nil {
			return fmt.Errorf("could not write the Rancher Desktop profile: %w", err)
		}
	}
	return nil
}

const rancherAdminAccessValue = "adminAccess"

// rancherProfileWritten reports whether this profile is already in the registry,
// so start-up doesn't shell out to reg on every launch for each of its values.
func rancherProfileWritten() bool {
	out, err := proc.Command("reg", "query", rancherProfileKey, "/v", "version").Output()
	if err != nil || !strings.Contains(string(out), fmt.Sprintf("0x%x", rancherProfileVersion)) {
		return false
	}
	return proc.Command("reg", "query", rancherProfileKey+`\application`,
		"/v", rancherAdminAccessValue).Run() != nil
}

func rancherSettings() []string {
	settings := []string{
		"--application.auto-start=false",
		"--application.start-in-background=true",
		"--application.updater.enabled=false",
		"--application.hide-notification-icon=true",
		"--application.telemetry.enabled=false",
		"--application.window.quit-on-close=false",
		"--diagnostics.connectivity.interval=0",
		"--container-engine.name=moby",
		"--kubernetes.enabled=false",
	}
	if runtime.GOOS != "windows" {
		settings = append(settings, "--application.admin-access=true")
	}
	return settings
}

// rancherLaunchArgs are what Rancher Desktop itself accepts on its command line.
// The profile only seeds a first run; these also settle an install that someone
// already answered the welcome dialog for.
func rancherLaunchArgs() []string {
	return append([]string{"--no-modal-dialogs"}, rancherSettings()...)
}

func applyRancherProfileNow() {
	rdctl := rdctlPath()
	if rdctl == "" {
		return
	}
	_ = proc.Command(rdctl, append([]string{"set"}, rancherSettings()...)...).Run()
}

func rdctlPath() string {
	var bundled string
	switch runtime.GOOS {
	case "darwin":
		bundled = filepath.Join(rancherAppMac, "Contents", "Resources", "resources", "darwin", "bin", "rdctl")
	case "windows":
		if exe := windowsRancherDesktopExe(); exe != "" {
			bundled = filepath.Join(filepath.Dir(exe), "resources", "resources", "win32", "bin", "rdctl.exe")
		}
	}
	if bundled != "" {
		if _, err := os.Stat(bundled); err == nil {
			return bundled
		}
	}
	if p, err := exec.LookPath("rdctl"); err == nil {
		return p
	}
	return ""
}
