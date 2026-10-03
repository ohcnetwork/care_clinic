package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func recoveryApp(t *testing.T) (*App, []string) {
	t.Helper()
	a := settingsApp(t)
	sheet, hashes, err := generateAdminRecoveryCodes("test-clinic.local")
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, line := range strings.Split(string(sheet), "\n") {
		if strings.HasPrefix(line, "[ ] ") {
			codes = append(codes, strings.TrimPrefix(line, "[ ] "))
		}
	}
	if len(codes) != 6 || len(hashes) != 6 {
		t.Fatal("expected exactly six codes")
	}
	cfg := a.loadConfig()
	cfg.AdminRecoveryHashes = hashes
	if err := a.saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	return a, codes
}

func TestAdminRecoverySingleUseAndPersistence(t *testing.T) {
	a, codes := recoveryApp(t)
	password := "NewDesktopPass123"
	normal := strings.ToLower(strings.ReplaceAll(codes[0], "-", " "))
	if err := a.ResetAdminPassword(normal, password); err != nil {
		t.Fatal(err)
	}
	if !a.VerifyAdminPassword(password) || a.VerifyAdminPassword(settingsPassword) {
		t.Fatal("password was not changed")
	}
	disk, err := loadConfig(a.configPath())
	if err != nil || disk.adminRecoveryCount() != 5 {
		t.Fatalf("used code was not durably consumed: %+v, %v", disk, err)
	}
	a.cfg = disk
	if err := a.ResetAdminPassword(codes[0], "AnotherDesktop123"); err == nil {
		t.Fatal("used code accepted after reload")
	}
	if err := a.ResetAdminPassword(codes[1], "AnotherDesktop123"); err != nil {
		t.Fatal("another code should remain valid:", err)
	}
	raw, err := os.ReadFile(a.configPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range append(codes, password, "AnotherDesktop123") {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("plaintext secret saved in config")
		}
	}
}

func TestAdminRecoveryCodeCountTracksDurableChanges(t *testing.T) {
	a, codes := recoveryApp(t)
	check := func(want int) {
		t.Helper()
		cfg, err := loadConfig(a.configPath())
		if err != nil {
			t.Fatal(err)
		}
		a.cfg = cfg
		count, err := a.GetAdminRecoveryCodeCount()
		if err != nil || count != want {
			t.Fatalf("remaining codes = %d, want %d: %v", count, want, err)
		}
	}
	check(6)
	for i, code := range codes {
		if err := a.ResetAdminPassword(code, "NewDesktopPass123"); err != nil {
			t.Fatal(err)
		}
		check(5 - i)
	}
	if err := a.ResetAdminPassword(codes[0], "NewDesktopPass123"); err == nil {
		t.Fatal("a used code was accepted")
	}
	check(0)
	path := filepath.Join(t.TempDir(), "new-codes.txt")
	if err := os.WriteFile(path, []byte("do not overwrite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.saveAdminRecoveryCodes(path); err == nil {
		t.Fatal("failed export succeeded")
	}
	check(0)
	if err := a.saveAdminRecoveryCodes(filepath.Join(t.TempDir(), "new-codes.txt")); err != nil {
		t.Fatal(err)
	}
	check(6)
	a.cfg.Role = roleClient
	if _, err := a.GetAdminRecoveryCodeCount(); err == nil {
		t.Fatal("client exposed server recovery status")
	}
}

func TestAdminRecoveryFailuresAndAtomicWrite(t *testing.T) {
	a, codes := recoveryApp(t)
	before := a.loadConfig()
	if err := a.ResetAdminPassword(codes[0], "weak"); err == nil {
		t.Fatal("weak password accepted")
	}
	if a.loadConfig().adminRecoveryCount() != 6 {
		t.Fatal("invalid new password consumed a code")
	}
	a.configFile = filepath.Join(t.TempDir(), "directory")
	if err := os.Mkdir(a.configFile, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := a.ResetAdminPassword(codes[0], "ChangedDesktop123"); err == nil {
		t.Fatal("failed persistence reported success")
	}
	after := a.loadConfig()
	if after.AdminPwHash != before.AdminPwHash || after.AdminRecoveryHashes != before.AdminRecoveryHashes {
		t.Fatal("failed write changed password or consumed a code in memory")
	}
}

func TestAdminRecoveryRateLimitSurvivesReload(t *testing.T) {
	a, codes := recoveryApp(t)
	for i := 0; i < 5; i++ {
		if err := a.ResetAdminPassword("incorrect", "NewDesktopPass123"); err == nil {
			t.Fatal("incorrect recovery code accepted")
		}
	}
	cfg, err := loadConfig(a.configPath())
	if err != nil {
		t.Fatal(err)
	}
	a.cfg = cfg
	if err := a.ResetAdminPassword(codes[0], "NewDesktopPass123"); err == nil || !strings.Contains(err.Error(), "try again") {
		t.Fatalf("lockout not enforced: %v", err)
	}
	cfg.RecoveryRetryAfter = time.Now().Add(-time.Second).Unix()
	if err := a.saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := a.ResetAdminPassword(codes[0], "NewDesktopPass123"); err != nil {
		t.Fatal(err)
	}
	if a.loadConfig().RecoveryFailures != 0 {
		t.Fatal("successful recovery did not reset throttle")
	}
}

func TestAdminRecoveryConcurrentUse(t *testing.T) {
	a, codes := recoveryApp(t)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- a.ResetAdminPassword(codes[0], "NewDesktopPass123")
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 || a.loadConfig().adminRecoveryCount() != 5 {
		t.Fatal("concurrent recovery reused a code")
	}
}

func TestChangeDesktopPasswordLeavesRecoveryAndWebSettings(t *testing.T) {
	a, codes := recoveryApp(t)
	path := filepath.Join(a.installDir(), "backend.env")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ChangeAdminPassword("wrong", "NewDesktopPass123"); err == nil {
		t.Fatal("unauthenticated password change accepted")
	}
	if err := a.ChangeAdminPassword(settingsPassword, "NewDesktopPass123"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("Desktop password change modified backend configuration")
	}
	if err := a.ResetAdminPassword(codes[0], settingsPassword); err != nil {
		t.Fatal("password change invalidated recovery codes:", err)
	}
}

func TestRecoveryExportNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovery.txt")
	if err := saveRecoveryFile(path, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := saveRecoveryFile(path, []byte("second")); err == nil {
		t.Fatal("existing export overwritten")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "first" {
		t.Fatal("existing export changed")
	}
}

func TestRecoverySetupLockedAfterInstallStarts(t *testing.T) {
	a, _ := recoveryApp(t)
	if err := a.requireRecoverySetup(); err == nil {
		t.Fatal("installed clinic allowed unauthenticated recovery replacement")
	}
	a.cfg.SetupDone = false
	if err := a.requireRecoverySetup(); err == nil {
		t.Fatal("partially installed clinic allowed recovery replacement")
	}
}

func TestRecoveryCodeReplacementRequiresSuccessfulExport(t *testing.T) {
	a, oldCodes := recoveryApp(t)
	before := a.loadConfig()
	path := filepath.Join(t.TempDir(), "new-codes.txt")
	if err := saveRecoveryFile(path, []byte("existing personal file")); err != nil {
		t.Fatal(err)
	}
	if err := a.saveAdminRecoveryCodes(path); err == nil {
		t.Fatal("failed export activated replacement codes")
	}
	if a.loadConfig() != before {
		t.Fatal("failed export invalidated the existing codes")
	}
	path = filepath.Join(t.TempDir(), "new-codes.txt")
	if err := a.saveAdminRecoveryCodes(path); err != nil {
		t.Fatal(err)
	}
	if err := a.ResetAdminPassword(oldCodes[0], "NewDesktopPass123"); err == nil {
		t.Fatal("replacement left old codes valid")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "[ ] ") {
			codes = append(codes, strings.TrimPrefix(line, "[ ] "))
		}
	}
	if len(codes) != 6 {
		t.Fatal("replacement sheet must contain six codes")
	}
	if err := a.ResetAdminPassword(codes[0], "NewDesktopPass123"); err != nil {
		t.Fatal("exported replacement code did not work:", err)
	}
}

func TestRecoveryMaterialLocationSeparation(t *testing.T) {
	a, _ := recoveryApp(t)
	backupParent := t.TempDir()
	for _, path := range []string{
		filepath.Join(a.installDir(), "key.pem"),
		filepath.Join(filepath.Dir(a.configPath()), "codes.txt"),
		filepath.Join(backupParent, "care-db-backups", "key.pem"),
	} {
		if err := a.recoveryLocation(path, backupParent); err == nil {
			t.Fatalf("unsafe recovery location accepted: %s", path)
		}
	}
	if err := a.recoveryLocation(filepath.Join(t.TempDir(), "key.pem"), backupParent); err != nil {
		t.Fatal(err)
	}
}
