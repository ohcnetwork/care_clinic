package prereq

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

const (
	rancherProfileDomain = "io.rancherdesktop.profile.defaults"
	rancherProfileKeyWin = `HKCU\Software\Policies\Rancher Desktop`
	rancherRCStart       = "### MANAGED BY RANCHER DESKTOP START (DO NOT EDIT)"
	rancherRCEnd         = "### MANAGED BY RANCHER DESKTOP END (DO NOT EDIT)"
)

func RancherDesktopInstalled() bool {
	return (runtime.GOOS == "darwin" || runtime.GOOS == "windows") && rancherDesktopInstalled()
}

func (pr *Provisioner) RemoveRancherDesktop() error {
	switch runtime.GOOS {
	case "darwin":
		return pr.removeRancherDarwin()
	case "windows":
		return pr.removeRancherWindows()
	default:
		pr.logln("Rancher Desktop is only installed by CARE Clinic on macOS and Windows; nothing to remove.")
		return nil
	}
}

func (pr *Provisioner) resetRancher() {
	rdctl := rdctlPath()
	if rdctl == "" {
		return
	}
	pr.logln("Stopping Rancher Desktop and clearing its virtual machine and settings...")
	_ = proc.Command(rdctl, "shutdown").Run()
	if out, err := proc.Command(rdctl, "factory-reset").CombinedOutput(); err != nil {
		pr.logln("rdctl factory-reset reported an error - removing its files directly: " +
			strings.TrimSpace(err.Error()+" "+string(out)))
	}
}

func (pr *Provisioner) removeRancherDarwin() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	pr.resetRancher()
	_ = proc.Command("osascript", "-e", `quit app "Rancher Desktop"`).Run()

	_ = proc.Command("defaults", "delete", rancherProfileDomain).Run()
	var failed []error
	for _, p := range darwinRancherUserPaths(home) {
		if err := os.RemoveAll(p); err != nil {
			failed = append(failed, err)
		}
	}
	for _, rc := range rancherShellRCFiles(home) {
		if err := stripRancherRC(rc); err != nil {
			failed = append(failed, err)
		}
	}

	if cmds := darwinRancherRootCmds(home); len(cmds) > 0 {
		pr.logln("Removing Rancher Desktop from Applications. macOS will ask for your password...")
		if err := elevate.Run(strings.Join(cmds, "; "), true); err != nil {
			failed = append(failed, fmt.Errorf("could not remove Rancher Desktop's system files: %w", err))
		}
	}
	if _, err := os.Stat(rancherAppMac); err == nil {
		failed = append(failed, errors.New("Rancher Desktop is still in Applications; drag it to the Trash to finish"))
	}
	if err := errors.Join(failed...); err != nil {
		return err
	}
	pr.logln("Rancher Desktop and its settings were removed.")
	return nil
}

func darwinRancherUserPaths(home string) []string {
	lib := filepath.Join(home, "Library")
	return []string{
		filepath.Join(lib, "Preferences", rancherProfileDomain+".plist"),
		filepath.Join(lib, "Preferences", "rancher-desktop"),
		filepath.Join(lib, "Preferences", "io.rancherdesktop.app.plist"),
		filepath.Join(lib, "Application Support", "rancher-desktop"),
		filepath.Join(lib, "Caches", "rancher-desktop"),
		filepath.Join(lib, "Caches", "io.rancherdesktop.app"),
		filepath.Join(lib, "Caches", "io.rancherdesktop.app.ShipIt"),
		filepath.Join(lib, "Logs", "rancher-desktop"),
		filepath.Join(lib, "Saved Application State", "io.rancherdesktop.app.savedState"),
		filepath.Join(home, ".rd"),
	}
}

func darwinRancherRootCmds(home string) []string {
	var cmds []string
	if _, err := os.Stat(dockerSockDaemon); err == nil {
		cmds = append(cmds,
			"launchctl bootout system/"+dockerSockLabel+" 2>/dev/null",
			"rm -f "+elevate.ShQuote(dockerSockDaemon))
	}
	if target, err := os.Readlink(dockerSockLink); err == nil && target == filepath.Join(home, ".rd", "docker.sock") {
		cmds = append(cmds, "rm -f "+dockerSockLink)
	}
	for _, p := range []string{rancherSudoersPath, rancherOldSudoers, rancherOptDir, rancherAppMac} {
		if _, err := os.Lstat(p); err == nil {
			cmds = append(cmds, "rm -rf "+elevate.ShQuote(p))
		}
	}
	cmds = append(cmds, rancherVMNetRemoveCmds(vmnetRunDir, processAlive)...)
	if len(cmds) == 0 {
		return nil
	}
	return append(cmds, "true")
}

func rancherShellRCFiles(home string) []string {
	names := []string{".bashrc", ".bash_profile", ".zshrc", ".zprofile", ".profile", ".cshrc", ".tcshrc",
		filepath.Join(".config", "fish", "config.fish")}
	paths := make([]string, 0, len(names))
	for _, n := range names {
		paths = append(paths, filepath.Join(home, n))
	}
	return paths
}

func stripRancherRC(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	stripped, changed := stripRancherBlock(string(data))
	if !changed {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(stripped), info.Mode().Perm())
}

func stripRancherBlock(s string) (string, bool) {
	lines := strings.SplitAfter(s, "\n")
	out := make([]string, 0, len(lines))
	inside, changed := false, false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == rancherRCStart:
			inside, changed = true, true
		case trimmed == rancherRCEnd && inside:
			inside = false
		case !inside:
			out = append(out, line)
		}
	}
	if inside {
		return s, false
	}
	return strings.Join(out, ""), changed
}

func (pr *Provisioner) removeRancherWindows() error {
	pr.resetRancher()
	_ = proc.Command("taskkill", "/IM", "Rancher Desktop.exe", "/F", "/T").Run()

	if windowsRancherDesktopExe() != "" {
		removed := false
		if hasCommand("winget") {
			pr.logln("Uninstalling Rancher Desktop with winget...")
			removed = pr.run.Run("winget", "uninstall", "-e", "--id", "SUSE.RancherDesktop",
				"--silent", "--accept-source-agreements") == nil && windowsRancherDesktopExe() == ""
		}
		if !removed {
			code, err := windowsRancherProductCode()
			if err != nil {
				return err
			}
			pr.logln("Running the Rancher Desktop uninstaller. Windows will ask for permission...")
			if err := pr.runElevated("msiexec", "/x", code, "/qn", "/norestart"); err != nil {
				return fmt.Errorf("could not uninstall Rancher Desktop: %w", err)
			}
		}
	}

	for _, distro := range []string{"rancher-desktop", "rancher-desktop-data"} {
		_ = proc.Command("wsl", "--unregister", distro).Run()
	}
	_ = proc.Command("reg", "delete", rancherProfileKeyWin, "/f").Run()

	var failed []error
	for _, base := range []string{os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA")} {
		if base == "" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(base, "rancher-desktop")); err != nil {
			failed = append(failed, err)
		}
	}
	if windowsRancherDesktopExe() != "" {
		failed = append(failed, errors.New("Rancher Desktop is still installed; remove it from Settings > Apps to finish"))
	}
	if err := errors.Join(failed...); err != nil {
		return err
	}
	pr.logln("Rancher Desktop and its settings were removed.")
	return nil
}

func windowsRancherProductCode() (string, error) {
	out, err := proc.Command("powershell", "-NoProfile", "-Command",
		`$ErrorActionPreference = 'SilentlyContinue'; `+
			`Get-ChildItem 'HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall','HKLM:\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall','HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall' | `+
			`Get-ItemProperty | Where-Object { $_.DisplayName -like 'Rancher Desktop*' -and $_.PSChildName -like '{*}' } | `+
			`Select-Object -First 1 -ExpandProperty PSChildName`).Output()
	if err != nil {
		return "", fmt.Errorf("could not find Rancher Desktop's uninstaller: %w", err)
	}
	code := strings.TrimSpace(string(out))
	if !strings.HasPrefix(code, "{") || !strings.HasSuffix(code, "}") {
		return "", errors.New("could not find Rancher Desktop's uninstaller; remove it from Settings > Apps")
	}
	return code, nil
}
