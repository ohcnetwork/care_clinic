package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/backup"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

func retrySetupApp(t *testing.T) (*App, []string) {
	t.Helper()
	a := roleApp(t)
	certificate, key, err := backup.GenerateRecoveryFile()
	if err != nil {
		t.Fatal(err)
	}
	recovery := filepath.Join(t.TempDir(), "recovery.pem")
	if err := saveRecoveryFile(recovery, key); err != nil {
		t.Fatal(err)
	}
	a.cfg = Config{
		Role: roleServer, MDNSName: "saved-clinic.local",
		BackupDir:         filepath.Join(t.TempDir(), "care-db-backups"),
		BackupCertificate: string(certificate), BackupRecoveryPath: recovery, BackupRecoveryVerified: true,
	}
	codes := filepath.Join(t.TempDir(), "codes.txt")
	if err := a.saveAdminRecoveryCodes(codes); err != nil {
		t.Fatal(err)
	}
	cfg := a.loadConfig()
	cfg.AdminPwHash = "previously-saved-password-hash"
	if err := a.saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	a.setupAttempt = &setupAttempt{
		config:  cfg,
		prepare: func() error { return errors.New("preparation is still incomplete") },
		start:   func() error { return errors.New("startup is still incomplete") },
	}
	return a, []string{a.configPath(), recovery, codes}
}

func TestSetupAttemptRetriesOnlyUnfinishedPhases(t *testing.T) {
	prepareCalls, startCalls := 0, 0
	attempt := &setupAttempt{
		prepare: func() error {
			prepareCalls++
			if prepareCalls == 1 {
				return &proc.NetworkError{Err: errors.New("download failed")}
			}
			return nil
		},
		start: func() error {
			startCalls++
			if startCalls == 1 {
				return errors.New("startup failed")
			}
			return nil
		},
	}
	if err := attempt.run(); err == nil || attempt.prepared || startCalls != 0 {
		t.Fatal("a failed preparation was treated as complete")
	}
	if err := attempt.run(); err == nil || !attempt.prepared || prepareCalls != 2 || startCalls != 1 {
		t.Fatal("retry didn't finish preparation before attempting startup")
	}
	if err := attempt.run(); err != nil || prepareCalls != 2 || startCalls != 2 {
		t.Fatalf("startup retry repeated completed preparation: %v", err)
	}
}

func TestRetrySetupPreservesConfigurationAndRecoveryMaterials(t *testing.T) {
	a, paths := retrySetupApp(t)
	cfg := a.loadConfig()
	before := make(map[string]string)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = string(data)
	}
	attempt := a.setupAttempt
	for range 2 {
		if err := a.RetrySetup(); err != nil {
			t.Fatal(err)
		}
		a.jobMu.Lock()
		if a.setupAttempt != attempt || a.loadConfig() != cfg {
			t.Fatal("retry replaced the setup or its saved choices")
		}
		a.jobMu.Unlock()
		for path, want := range before {
			got, err := os.ReadFile(path)
			if err != nil || string(got) != want {
				t.Fatalf("retry changed a saved setup file %s: %v", filepath.Base(path), err)
			}
		}
	}
}

func TestRetrySetupRejectsDifferentOrCompletedSetups(t *testing.T) {
	for _, change := range []struct {
		name string
		run  func(*App)
	}{
		{"no retained attempt", func(a *App) { a.setupAttempt = nil }},
		{"installed", func(a *App) { a.cfg.SetupDone = true }},
		{"removing", func(a *App) { a.cfg.Removing = true }},
		{"password changed", func(a *App) { a.cfg.AdminPwHash = "different" }},
		{"address changed", func(a *App) { a.cfg.MDNSName = "another.local" }},
		{"backup folder changed", func(a *App) { a.cfg.BackupDir = filepath.Join(t.TempDir(), "different") }},
		{"backup certificate changed", func(a *App) { a.cfg.BackupCertificate = "different" }},
		{"recovery codes changed", func(a *App) { a.cfg.AdminRecoveryHashes[0] = "different" }},
		{"client", func(a *App) { a.cfg = Config{Role: roleClient} }},
		{"unchosen role", func(a *App) { a.cfg = Config{} }},
	} {
		t.Run(change.name, func(t *testing.T) {
			a, _ := retrySetupApp(t)
			ran := false
			a.setupAttempt.prepare = func() error { ran = true; return errors.New("must not run") }
			change.run(a)
			before := a.loadConfig()
			if err := a.RetrySetup(); err == nil {
				t.Fatal("an invalid retry was accepted")
			}
			if ran || a.loadConfig() != before {
				t.Fatal("a rejected retry changed the installation")
			}
			if !a.jobMu.TryLock() {
				t.Fatal("a rejected retry retained the job lock")
			}
			a.jobMu.Unlock()
		})
	}
}

func TestRetrySetupRequiresTheOriginalRecoveryFiles(t *testing.T) {
	for _, file := range []string{"backup", "admin"} {
		for _, problem := range []string{"missing", "changed"} {
			t.Run(file+"/"+problem, func(t *testing.T) {
				a, _ := retrySetupApp(t)
				path := a.cfg.BackupRecoveryPath
				if file == "admin" {
					path = a.cfg.AdminRecoveryPath
				}
				var err error
				if problem == "missing" {
					err = os.Remove(path)
				} else {
					err = os.WriteFile(path, []byte("not the original recovery file"), 0o600)
				}
				if err != nil {
					t.Fatal(err)
				}
				before := a.loadConfig()
				if err := a.RetrySetup(); err == nil {
					t.Fatal("retry accepted an unavailable or changed recovery file")
				}
				if a.loadConfig() != before {
					t.Fatal("an invalid recovery file reset the saved choices")
				}
			})
		}
	}
}

func TestRetrySetupRequiresTheOriginalBackupLocation(t *testing.T) {
	a, _ := retrySetupApp(t)
	if err := os.Remove(filepath.Dir(a.cfg.BackupDir)); err != nil {
		t.Fatal(err)
	}
	if err := a.RetrySetup(); err == nil {
		t.Fatal("retry accepted an unavailable backup location")
	}
}

func TestRetrySetupValidatesTheDefaultBackupLocation(t *testing.T) {
	a, _ := retrySetupApp(t)
	cfg := a.loadConfig()
	cfg.BackupDir = ""
	if err := a.saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	a.setupAttempt.config = cfg
	if err := a.validateSetupRetry(); err != nil {
		t.Fatalf("the default backup location was treated as a relative path: %v", err)
	}
}

func TestRetrySetupRespectsTheJobAndClosingGuards(t *testing.T) {
	a, _ := retrySetupApp(t)
	a.jobMu.Lock()
	if err := a.RetrySetup(); err == nil {
		t.Fatal("retry bypassed another running job")
	}
	a.jobMu.Unlock()
	a.closing = true
	if err := a.RetrySetup(); err == nil {
		t.Fatal("retry ran while CARE Desktop was closing")
	}
}

func TestSetupFailureUsesTheFailedCommandsCause(t *testing.T) {
	a, _ := retrySetupApp(t)
	network := fmt.Errorf("caddy image: %w", &proc.NetworkError{Err: errors.New("exit status 1")})
	if got := a.setupFailure(network); !got.CanRetry || !got.DownloadInterrupted {
		t.Fatalf("interrupted download lost its retry metadata: %+v", got)
	}
	if got := a.setupFailure(errors.New("syntax error")); !got.CanRetry || got.DownloadInterrupted {
		t.Fatalf("a genuine build error was called an internet failure: %+v", got)
	}
	a.setupAttempt = nil
	if got := a.setupFailure(network); got.CanRetry || !got.DownloadInterrupted {
		t.Fatalf("an unprepared setup gained a retry from its error text: %+v", got)
	}
}
