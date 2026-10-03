package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMacUpdateHelperWaitsBeforeReplacingAndReportsFailure(t *testing.T) {
	target := "/Applications/CARE's Desktop.app"
	staged := "/private/update/stage/CARE's Desktop.app"
	work := "/private/update"
	for _, elevated := range []bool{false, true} {
		script := macUpdateHelper(target, staged, work, 12345, elevated)
		wait := strings.Index(script, "/bin/kill -0 12345")
		swap := strings.Index(script, "owner=")
		if wait < 0 || swap < wait || !strings.Contains(script, "did not exit; nothing was replaced") {
			t.Fatal("replacement must follow a bounded wait for exit")
		}
		for _, required := range []string{"'ready'", "'continue'", "helper.log", "display alert", "/usr/bin/open", "codesign --verify"} {
			// Path quoting includes the full path, not individual components.
			required = strings.Trim(required, "'")
			if !strings.Contains(script, required) {
				t.Errorf("helper missing %q", required)
			}
		}
		if strings.Contains(script, "xattr") || strings.Contains(script, "docker") {
			t.Fatal("helper must not bypass quarantine or stop the clinic")
		}
		if runtime.GOOS != "windows" {
			cmd := exec.Command("/bin/sh", "-n")
			cmd.Stdin = strings.NewReader(script)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("invalid shell helper: %v: %s", err, out)
			}
		}
	}
}

func TestWindowsUpdateHelperUsesExactInstallDirectoryAndWaits(t *testing.T) {
	exe := filepath.Join("custom path", "CARE's Desktop.exe")
	script := windowsUpdateHelper(exe, "release-setup.exe", "work", "1.2.3", strings.Repeat("a", 64), 12345)
	for _, required := range []string{
		"Get-Process -Id 12345", "UninstallString", "Confirm-Download",
		"SignerCertificate.Subject", "Get-FileHash", "WaitForExit(120000)",
		"/S /CAREUPDATE=1 /D=" + filepath.Dir(exe), "-Verb RunAs",
		"$setup.ExitCode -ne 0", "ProductVersion", "MessageBox", "helper.log",
	} {
		if !strings.Contains(script, required) {
			t.Errorf("helper missing %q", required)
		}
	}
	if strings.Index(script, "WaitForExit(120000)") > strings.Index(script, "$setup = Start-Process") {
		t.Fatal("installer started before the app exited")
	}
	if strings.Contains(script, "Stop-Process") || strings.Contains(script, "docker") {
		t.Fatal("helper must not kill applications or stop clinic containers")
	}
}

func TestUpdateHelperStartupFailureKeepsAppOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "helper.log"), []byte("permission denied"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := startUpdateHelper(exec.Command("/bin/sh", "-c", "exit 1"), filepath.Join(work, "ready"))
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("helper failure not surfaced: %v", err)
	}
}

func TestUpdateHelperRequiresReadinessAcknowledgement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command("/bin/sh", "-c", `printf ready > "$1"; read token`, "helper", ready)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close() }()
	if err := startUpdateHelper(cmd, ready); err != nil {
		t.Fatal(err)
	}
	// This fixture waits for stdin, never runs an installer, and exits on EOF.
	if _, err := os.Stat(ready); err != nil {
		t.Fatal("readiness was not acknowledged")
	}
}

func TestMacCopiedBundlePinsExecutableBeforeSwap(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("PlistBuddy is macOS-only")
	}
	work := t.TempDir()
	stage := filepath.Join(work, "stage", "CARE.app")
	if err := os.MkdirAll(filepath.Join(stage, "Contents", "MacOS"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "Contents", "Info.plist"), []byte(
		`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>CARE's Desktop</string></dict></plist>`), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(stage, "Contents", "MacOS", "CARE's Desktop")
	if err := os.WriteFile(exe, []byte("inert executable fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	check, err := macCopiedBundleVerification(stage)
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := fileSHA256(exe)
	if !strings.Contains(check, digest) || !strings.Contains(check, "codesign --verify --deep --strict") {
		t.Fatal("copied bundle must retain the verified executable and valid resource signature")
	}
	for _, elevated := range []bool{false, true} {
		cmd := exec.Command("/bin/sh", "-n")
		cmd.Stdin = strings.NewReader(macUpdateHelper("/Applications/CARE.app", stage, work, 12345, elevated, check))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("invalid verified helper shell: %v: %s", err, out)
		}
	}
}

func TestSwapNeverDeletesPreexistingRecoveryFolder(t *testing.T) {
	script := swapScript("/Applications/CARE.app", "/cache/update-123/stage/CARE.app")
	if !strings.Contains(script, "/bin/mkdir -m 700") || strings.Contains(script, ".previous") {
		t.Fatal("swap must reserve a private sibling rather than deleting a previous recovery copy")
	}
	if strings.Index(script, "ditto") > strings.Index(script, "/bin/mv '/Applications/CARE.app'") {
		t.Fatal("must stage the complete replacement before moving the current app")
	}
}
