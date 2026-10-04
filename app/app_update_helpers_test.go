package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMacReplacementElevationPreservesOwnership(t *testing.T) {
	for _, tc := range []struct {
		name     string
		writable bool
		owner    string
		uid      int
		groups   []int
		want     bool
	}{
		{"user-owned writable", true, "501:20", 501, []int{20, 80}, false},
		{"root-owned writable", true, "0:80", 501, []int{20, 80}, true},
		{"other-user-owned writable", true, "502:20", 501, []int{20, 80}, true},
		{"inaccessible group", true, "501:0", 501, []int{20, 80}, true},
		{"supplementary group", true, "501:80", 501, []int{20, 80}, false},
		{"protected parent", false, "501:20", 501, []int{20, 80}, true},
		{"already root", true, "501:20", 0, []int{0}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			elevated := macReplacementNeedsElevation(tc.writable, tc.owner, tc.uid, tc.groups)
			if elevated != tc.want {
				t.Fatalf("elevated = %v, want %v", elevated, tc.want)
			}
			script := macUpdateHelper("/Applications/CARE.app", "/cache/stage/CARE.app", "/cache", 12345, elevated)
			if strings.Contains(script, "with administrator privileges") != tc.want {
				t.Fatal("helper does not use the required privilege level")
			}
		})
	}
}

func TestMacUpdateElevationPreflight(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS stat format")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "CARE.app")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	elevated, err := macUpdateNeedsElevation(target)
	if err != nil || elevated {
		t.Fatalf("user-owned writable fixture needs no elevation: %v, %v", elevated, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("preflight left write probes behind: %v, %v", entries, err)
	}
	if _, err := macUpdateNeedsElevation(filepath.Join(dir, "missing.app")); err == nil {
		t.Fatal("unreadable ownership must fail before update handoff")
	}
}

func TestMacUpdateHelperWaitsBeforeReplacingAndReportsFailure(t *testing.T) {
	target := "/Applications/CARE's Clinic.app"
	staged := "/private/update/stage/CARE's Clinic.app"
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
	exe := filepath.Join("custom path", "CARE's Clinic.exe")
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
		`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>CARE's Clinic</string></dict></plist>`), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(stage, "Contents", "MacOS", "CARE's Clinic")
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
