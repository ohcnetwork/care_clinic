package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/appremoval"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

var errNoInPlaceUpdate = errors.New("CARE Desktop can't replace itself where it is installed, so the download is opened instead")

const macUpdateStepTimeout = 5 * time.Minute

func (a *App) replaceMacApp(dmg, version string) (err error) {
	target, err := appremoval.Target()
	if err != nil {
		return fmt.Errorf("%w (%v)", errNoInPlaceUpdate, err)
	}
	work := filepath.Dir(dmg)
	defer func() {
		if err != nil {
			_ = os.RemoveAll(work)
		}
	}()

	a.updateProgress(phaseVerifying, 0, 0)
	a.logln("Unpacking CARE Desktop " + version + "...")
	staged, err := stageMacBundle(dmg, work, filepath.Base(target))
	if err != nil {
		return err
	}
	if err := verifyMacBundle(staged, target, version); err != nil {
		return err
	}

	a.updateProgress(phaseInstalling, 0, 0)
	a.logln("Replacing " + target + "...")
	elevated := !canWrite(filepath.Dir(target))
	if elevated {
		a.logln("The app folder needs an administrator to change it; macOS will ask for a password.")
	}
	if err := elevate.Run(swapScript(target, staged), elevated); err != nil {
		return fmt.Errorf("couldn't replace CARE Desktop with version %s, so the current version was kept: %w", version, err)
	}

	relaunch := fmt.Sprintf("while kill -0 %d 2>/dev/null; do sleep 0.5; done; open %s; rm -rf %s",
		os.Getpid(), elevate.ShQuote(target), elevate.ShQuote(work))
	if err := proc.Command("/bin/sh", "-c", relaunch).Start(); err != nil {
		a.logln("CARE Desktop " + version + " is installed but couldn't reopen itself (" + err.Error() + "). Open it again from Applications.")
	}
	a.updateProgress(phaseRestarting, 0, 0)
	a.logln("CARE Desktop " + version + " is installed. Restarting...")
	a.closing = true
	a.quitAfterJob()
	return nil
}

func stageMacBundle(dmg, work, name string) (string, error) {
	mount := filepath.Join(work, "mnt")
	if err := os.MkdirAll(mount, 0o700); err != nil {
		return "", err
	}
	if err := macStep("hdiutil", "attach", "-nobrowse", "-noautoopen", "-readonly", "-mountpoint", mount, dmg); err != nil {
		return "", fmt.Errorf("couldn't open the downloaded disk image: %w", err)
	}
	defer func() { _ = macStep("hdiutil", "detach", "-force", mount) }()
	bundles, _ := filepath.Glob(filepath.Join(mount, "*.app"))
	if len(bundles) != 1 {
		return "", fmt.Errorf("the downloaded disk image holds %d apps, expected exactly one", len(bundles))
	}
	staged := filepath.Join(work, "stage", name)
	if err := os.MkdirAll(filepath.Dir(staged), 0o700); err != nil {
		return "", err
	}
	if err := macStep("ditto", bundles[0], staged); err != nil {
		return "", fmt.Errorf("couldn't copy the new version out of the disk image: %w", err)
	}
	return staged, nil
}

func verifyMacBundle(staged, current, version string) error {
	if err := macStep("codesign", "--verify", "--deep", "--strict", staged); err != nil {
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
	return nil
}

func swapScript(target, staged string) string {
	t := elevate.ShQuote(target)
	o := elevate.ShQuote(filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".previous"))
	s := elevate.ShQuote(staged)
	return strings.Join([]string{
		"set -e",
		"owner=$(stat -f %u:%g " + t + ")",
		"rm -rf " + o,
		"mv " + t + " " + o,
		"if ! ditto " + s + " " + t + "; then rm -rf " + t + "; mv " + o + " " + t + "; exit 1; fi",
		"chown -R \"$owner\" " + t + " 2>/dev/null || true",
		"xattr -dr com.apple.quarantine " + t + " 2>/dev/null || true",
		"rm -rf " + o + " 2>/dev/null || true",
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
