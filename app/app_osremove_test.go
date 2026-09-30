package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemovalExitCodeFollowsSetupState(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg  Config
		want int
	}{
		"never chosen":            {Config{}, exitRemovable},
		"server chosen only":      {Config{Role: roleServer, MDNSName: "clinic.local"}, exitRemovable},
		"client chosen only":      {Config{Role: roleClient}, exitRemovable},
		"recovery kit prepared":   {Config{Role: roleServer, BackupCertificate: "public", BackupRecoveryPath: "/external/recovery.pem", BackupRecoveryVerified: true, AdminRecoveryHashes: [6]string{"hash"}}, exitRemovable},
		"installed server":        {Config{Role: roleServer, SetupDone: true, MDNSName: "clinic.local"}, exitSetUp},
		"partial server":          {Config{Role: roleServer, AdminPwHash: "partial"}, exitSetUp},
		"removing server":         {Config{Role: roleServer, Removing: true}, exitSetUp},
		"connected client":        {Config{Role: roleClient, ClientURL: "https://clinic.local"}, exitSetUp},
		"client owning trust":     {Config{Role: roleClient, ClientCertificateOwned: true}, exitSetUp},
		"client with certificate": {Config{Role: roleClient, ClientCertificate: "pinned"}, exitSetUp},
	} {
		t.Run(name, func(t *testing.T) {
			a := roleApp(t)
			if err := a.saveConfig(tc.cfg); err != nil {
				t.Fatal(err)
			}
			if got := a.removalExitCode(); got != tc.want {
				t.Fatalf("removalExitCode() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRemovalExitCodeKeepsInstallFiles(t *testing.T) {
	a := roleApp(t)
	if err := a.SelectRole(roleServer); err != nil {
		t.Fatal(err)
	}
	dir := a.installDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := a.removalExitCode(); got != exitSetUp {
		t.Fatalf("an installation with files on disk was reported removable (exit %d)", got)
	}
}

func TestRemoveAppRefusesWhileSetUp(t *testing.T) {
	a := roleApp(t)
	if err := a.saveConfig(Config{Role: roleClient, ClientURL: "https://clinic.local"}); err != nil {
		t.Fatal(err)
	}
	err := a.RemoveApp()
	if err == nil || !strings.Contains(err.Error(), "disconnect this computer") {
		t.Fatalf("RemoveApp() = %v, want the still-set-up refusal", err)
	}
	if a.removeTarget != "" {
		t.Fatalf("scheduled removal of %q while still set up", a.removeTarget)
	}
}

func TestOSUninstallerCannotRemoveAppAgain(t *testing.T) {
	a := roleApp(t)
	a.osUninstall = true
	if a.CanRemoveApp() {
		t.Fatal("offered in-app removal while the OS uninstaller is running")
	}
	if err := a.RemoveApp(); err == nil {
		t.Fatal("started app removal from inside the OS uninstaller")
	}
}
