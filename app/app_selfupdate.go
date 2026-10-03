package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/appremoval"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

const macUpdateStepTimeout = 5 * time.Minute

func updateTarget() (string, error) {
	target, err := appremoval.Target()
	if err != nil {
		return "", fmt.Errorf("update location unavailable: install CARE Desktop in a permanent folder first, then open that copy and update again (%w)", err)
	}
	if runtime.GOOS == "windows" {
		target = filepath.Dir(target)
	}
	return target, nil
}

// The helper acknowledges readiness before we quit, then waits for this exact
// process to exit. Nothing replaces the application while Wails is still using it.
func (a *App) handoffAppUpdate(download, version, digest string) (err error) {
	work := filepath.Dir(download)
	defer func() {
		if err != nil {
			_ = os.RemoveAll(work)
		}
	}()
	target, err := updateTarget()
	if err != nil {
		return err
	}
	ready := filepath.Join(work, "ready")
	commit := filepath.Join(work, "continue")
	var script, name string
	var cmd *exec.Cmd
	a.updateProgress(phaseVerifying, 0, 0)
	switch runtime.GOOS {
	case "darwin":
		a.logln("Unpacking CARE Desktop " + version + "...")
		staged, err := stageMacBundle(download, work, filepath.Base(target))
		if err != nil {
			return err
		}
		if err := verifyMacBundle(staged, target, version); err != nil {
			return err
		}
		verification, err := macCopiedBundleVerification(staged)
		if err != nil {
			return err
		}
		script = macUpdateHelper(target, staged, work, os.Getpid(), !canWrite(filepath.Dir(target)), verification)
		name = filepath.Join(work, "install.sh")
		cmd = proc.Command("/bin/sh", name)
	case "windows":
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		exe, err = filepath.EvalSymlinks(exe)
		if err != nil {
			return err
		}
		script = windowsUpdateHelper(exe, download, work, version, digest, os.Getpid())
		name = filepath.Join(work, "install.ps1")
		cmd = proc.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", name)
	default:
		return fmt.Errorf("in-place updates are not supported on %s", runtime.GOOS)
	}
	if err := os.WriteFile(name, []byte(script), 0o600); err != nil {
		return err
	}
	a.updateProgress(phaseInstalling, 0, 0)
	a.logln("Preparing an in-place update of " + target + ". Helper log: " + filepath.Join(work, "helper.log"))
	if err := startUpdateHelper(cmd, ready); err != nil {
		return err
	}
	if err := os.WriteFile(commit, []byte("continue"), 0o600); err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("couldn't authorize the update handoff: %w", err)
	}
	a.updateProgress(phaseRestarting, 0, 0)
	a.logln("Restarting CARE Desktop to finish updating. Clinic containers and data are left alone.")
	a.closing = true
	a.quitAfterJob()
	return nil
}

func startUpdateHelper(cmd *exec.Cmd, ready string) error {
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("couldn't start the update helper: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			detail, _ := os.ReadFile(filepath.Join(filepath.Dir(ready), "helper.log"))
			return fmt.Errorf("the update helper stopped before it was ready (%v): %s", err, strings.TrimSpace(string(detail)))
		case <-deadline.C:
			_ = cmd.Process.Kill()
			<-done
			return fmt.Errorf("the update helper did not become ready; CARE Desktop was kept open")
		case <-ticker.C:
			if _, err := os.Stat(ready); err == nil {
				return nil
			}
		}
	}
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	digest := sha256.New()
	if _, err := io.Copy(digest, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func stageMacBundle(dmg, work, name string) (staged string, err error) {
	mount := filepath.Join(work, "mnt")
	if err := os.MkdirAll(mount, 0o700); err != nil {
		return "", err
	}
	if err := macStep("/usr/bin/hdiutil", "attach", "-nobrowse", "-noautoopen", "-readonly", "-mountpoint", mount, dmg); err != nil {
		_ = macStep("/usr/bin/hdiutil", "detach", "-force", mount)
		return "", fmt.Errorf("couldn't open the downloaded disk image: %w", err)
	}
	defer func() {
		if detachErr := macStep("/usr/bin/hdiutil", "detach", "-force", mount); err == nil && detachErr != nil {
			err = fmt.Errorf("couldn't detach the update disk image: %w", detachErr)
		}
	}()
	bundles, _ := filepath.Glob(filepath.Join(mount, "*.app"))
	if len(bundles) != 1 {
		return "", fmt.Errorf("the downloaded disk image holds %d apps, expected exactly one", len(bundles))
	}
	staged = filepath.Join(work, "stage", name)
	if err := os.MkdirAll(filepath.Dir(staged), 0o700); err != nil {
		return "", err
	}
	if err := macStep("/usr/bin/ditto", bundles[0], staged); err != nil {
		return "", fmt.Errorf("couldn't copy the new version out of the disk image: %w", err)
	}
	return staged, nil
}

func verifyMacBundle(staged, current, version string) error {
	if err := macStep("/usr/bin/codesign", "--verify", "--deep", "--strict", staged); err != nil {
		return fmt.Errorf("the new version's code signature is not intact: %w", err)
	}
	got, err := plistValue(staged, "CFBundleShortVersionString")
	if err != nil {
		return err
	}
	if got != version {
		return fmt.Errorf("the downloaded app says it is version %s, not %s", got, version)
	}
	newID, err := plistValue(staged, "CFBundleIdentifier")
	if err != nil {
		return err
	}
	oldID, err := plistValue(current, "CFBundleIdentifier")
	if err != nil {
		return err
	}
	if newID != oldID {
		return fmt.Errorf("the downloaded app is %s, not %s", newID, oldID)
	}
	// Ad-hoc releases remain supported, but a Developer ID installation must
	// never silently downgrade to an ad-hoc or differently signed application.
	oldTeam, err := macSigningTeam(current)
	if err != nil {
		return err
	}
	newTeam, err := macSigningTeam(staged)
	if err != nil {
		return err
	}
	if oldTeam != "" && oldTeam != newTeam {
		return fmt.Errorf("the update's signing team does not match this installation")
	}
	if oldTeam != "" {
		if err := macStep("/usr/sbin/spctl", "--assess", "--type", "execute", staged); err != nil {
			return fmt.Errorf("macOS did not approve the signed update: %w", err)
		}
	}
	return nil
}

func macSigningTeam(bundle string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), macUpdateStepTimeout)
	defer cancel()
	out, err := proc.CommandContext(ctx, "/usr/bin/codesign", "-dv", "--verbose=4", bundle).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("couldn't read the app's signing identity: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if team, ok := strings.CutPrefix(line, "TeamIdentifier="); ok && team != "not set" {
			return team, nil
		}
	}
	return "", nil
}

// Pin the verified Mach-O, which seals Info.plist and the resource envelope.
// Recheck after copying into the private sibling, including when that copy is
// made by an administrator from the user's writable staging directory.
func macCopiedBundleVerification(staged string) (string, error) {
	executable, err := plistValue(staged, "CFBundleExecutable")
	if err != nil {
		return "", err
	}
	if executable == "" || executable == "." || filepath.Base(executable) != executable || strings.ContainsAny(executable, `/\`) {
		return "", fmt.Errorf("the update has an unsafe bundle executable name")
	}
	digest, err := fileSHA256(filepath.Join(staged, "Contents", "MacOS", executable))
	if err != nil {
		return "", err
	}
	return `test ! -L "$slot/new.app/Contents" && test ! -L "$slot/new.app/Contents/MacOS" && test ! -L "$slot/new.app/Contents/MacOS/"` +
		elevate.ShQuote(executable) + ` && test "$(/usr/bin/shasum -a 256 "$slot/new.app/Contents/MacOS/"` + elevate.ShQuote(executable) +
		` | /usr/bin/awk '{print $1}')" = ` + elevate.ShQuote(digest) +
		` && /usr/bin/codesign --verify --deep --strict "$slot/new.app"`, nil
}

func swapScript(target, staged string, verification ...string) string {
	t := elevate.ShQuote(target)
	// A private sibling gives rename its same-filesystem atomicity. Never remove
	// a pre-existing ".previous" directory: it may be somebody's recovery copy.
	s := elevate.ShQuote(staged)
	check := ":"
	if len(verification) > 0 {
		check = verification[0]
	}
	return strings.Join([]string{
		"set -e",
		"owner=$(/usr/bin/stat -f %u:%g " + t + ")",
		"slot=" + elevate.ShQuote(filepath.Join(filepath.Dir(target), ".care-update-"+filepath.Base(filepath.Dir(filepath.Dir(staged))))),
		"/bin/mkdir -m 700 \"$slot\"",
		"if ! /usr/bin/ditto " + s + " \"$slot/new.app\"; then /bin/rm -rf \"$slot\"; exit 1; fi",
		"if ! ( " + check + " ); then /bin/rm -rf \"$slot\"; exit 1; fi",
		"/usr/sbin/chown -R \"$owner\" \"$slot/new.app\"",
		"if ! /bin/mv " + t + " \"$slot/previous.app\"; then /bin/rm -rf \"$slot\"; exit 1; fi",
		"if ! /bin/mv \"$slot/new.app\" " + t + "; then /bin/mv \"$slot/previous.app\" " + t + "; exit 1; fi",
		"/bin/rm -rf \"$slot\"",
	}, "\n")
}

func plistValue(bundle, key string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), macUpdateStepTimeout)
	defer cancel()
	out, err := proc.CommandContext(ctx, "/usr/libexec/PlistBuddy", "-c", "Print :"+key,
		filepath.Join(bundle, "Contents", "Info.plist")).Output()
	if err != nil {
		return "", fmt.Errorf("couldn't read %s from %s: %w", key, bundle, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func macStep(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), macUpdateStepTimeout)
	defer cancel()
	out, err := proc.CommandContext(ctx, name, args...).CombinedOutput()
	if msg := strings.TrimSpace(string(out)); err != nil && msg != "" {
		return fmt.Errorf("%w: %s", err, msg)
	}
	return err
}

func canWrite(dir string) bool {
	f, err := os.CreateTemp(dir, ".care-write-check-"+strconv.Itoa(os.Getpid())+"-*")
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return true
}
