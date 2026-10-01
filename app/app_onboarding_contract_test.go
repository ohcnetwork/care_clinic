package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/clinic"
)

// Check the bridge signatures without opening a native dialog.
var _ interface {
	ChooseFolder(string) (string, error)
	ChooseBackupFile() (string, error)
} = (*App)(nil)

func onboardingConfigUnchanged(t *testing.T, a *App) func() {
	t.Helper()
	before := a.loadConfig()
	path := a.configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		current, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if a.loadConfig() != before || !bytes.Equal(data, after) {
			t.Fatal("read or rejected operation changed recovery/settings state")
		}
		if !os.SameFile(info, current) || !info.ModTime().Equal(current.ModTime()) {
			t.Fatal("read or rejected operation rewrote the settings file")
		}
	}
}

func onboardingJobUnlocked(t *testing.T, a *App) {
	t.Helper()
	if !a.jobMu.TryLock() {
		t.Fatal("operation retained the job lock")
	}
	a.jobMu.Unlock()
}

func TestOnboardingBackupPolicyRequiresAnInstalledServer(t *testing.T) {
	for _, state := range []string{"unchosen", "client", "preinstall", "partial", "removing", "missing compose", "directory compose"} {
		t.Run(state, func(t *testing.T) {
			a := roleApp(t)
			cfg := Config{Role: roleServer, SetupDone: true}
			switch state {
			case "unchosen":
				cfg = Config{}
			case "client":
				cfg = Config{Role: roleClient, ClientURL: "https://clinic.local"}
			case "preinstall":
				cfg.SetupDone = false
			case "partial":
				cfg.SetupDone, cfg.AdminPwHash = false, "unfinished"
			case "removing":
				cfg.Removing = true
			}
			if err := a.saveConfig(cfg); err != nil {
				t.Fatal(err)
			}
			unchanged := onboardingConfigUnchanged(t, a)
			if err := os.MkdirAll(a.installDir(), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(a.installDir(), "backend.env"), []byte("DB_BACKUP_RETENTION_PERIOD=37\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			composePath := filepath.Join(a.installDir(), "docker-compose.yml")
			if state == "directory compose" {
				if err := os.Mkdir(composePath, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if state != "missing compose" {
				if err := os.WriteFile(composePath, []byte("name: fixture\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			policy, err := a.GetBackupPolicy()
			if err == nil || policy != (clinic.BackupPolicy{}) {
				t.Fatalf("%s returned a usable policy: %+v, %v", state, policy, err)
			}
			want := map[string]string{
				"unchosen":          "clinic server",
				"client":            "clinic server",
				"preinstall":        "not set up",
				"partial":           "not set up",
				"removing":          "cleanup is incomplete",
				"missing compose":   "compose file is unavailable",
				"directory compose": "not a regular file",
			}[state]
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("%s bypassed its specific guard: %v", state, err)
			}
			unchanged()
			onboardingJobUnlocked(t, a)
		})
	}
}

func TestOnboardingBackupPolicyReadsCurrentInstalledSettings(t *testing.T) {
	a := settingsApp(t)
	if err := a.saveConfig(a.loadConfig()); err != nil {
		t.Fatal(err)
	}
	unchanged := onboardingConfigUnchanged(t, a)
	for _, tc := range []struct {
		body string
		days int
	}{
		{"DB_BACKUP_RETENTION_PERIOD=37\n", 37},
		{"DB_BACKUP_RETENTION_PERIOD=0\n", 0},
		{"UNRELATED=preserved\n", 0},
	} {
		path := filepath.Join(a.installDir(), "backend.env")
		if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
			t.Fatal(err)
		}
		policy, err := a.GetBackupPolicy()
		if err != nil || policy.IntervalSeconds != 86400 || policy.RetentionDays != tc.days {
			t.Fatalf("policy did not reflect backend.env: %+v, %v", policy, err)
		}
		body, err := os.ReadFile(path)
		if err != nil || string(body) != tc.body {
			t.Fatalf("policy read changed backend.env: %q, %v", body, err)
		}
		unchanged()
	}
	if err := os.Remove(filepath.Join(a.installDir(), "backend.env")); err != nil {
		t.Fatal(err)
	}
	if policy, err := a.GetBackupPolicy(); err == nil || policy != (clinic.BackupPolicy{}) {
		t.Fatalf("missing settings became a keep-forever policy: %+v, %v", policy, err)
	}
	unchanged()
}

func TestOnboardingRecoverySheetRejectsUnavailableOrIncompleteCodes(t *testing.T) {
	for _, kind := range []string{"empty", "duplicate", "used code", "malformed code", "directory", "oversized", "permission denied"} {
		t.Run(kind, func(t *testing.T) {
			a := roleApp(t)
			a.cfg = Config{Role: roleServer, MDNSName: "fixture.local"}
			path := filepath.Join(t.TempDir(), "codes.txt")
			if err := a.saveAdminRecoveryCodes(path); err != nil {
				t.Fatal(err)
			}
			unchanged := onboardingConfigUnchanged(t, a)
			sheet, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var lines []string
			for _, line := range strings.Split(string(sheet), "\n") {
				if strings.HasPrefix(line, "[ ] ") {
					lines = append(lines, line)
				}
			}
			if len(lines) != 6 {
				t.Fatal("fixture needs six exported codes")
			}
			want := "mismatch"
			switch kind {
			case "empty":
				sheet = nil
			case "duplicate":
				sheet = []byte(strings.Replace(string(sheet), lines[5], lines[0], 1))
			case "used code":
				sheet = []byte(strings.Replace(string(sheet), lines[0], strings.Replace(lines[0], "[ ]", "[x]", 1), 1))
			case "malformed code":
				sheet = []byte(strings.Replace(string(sheet), lines[0], "[ ] not-a-recovery-code", 1))
			case "directory":
				want = "unreadable"
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				want = "unreadable"
				sheet = append(sheet, bytes.Repeat([]byte(" "), 64*1024)...)
			case "permission denied":
				if runtime.GOOS == "windows" {
					t.Skip("permission bits do not deny Windows reads")
				}
				want = "unreadable"
				if err := os.Chmod(path, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
				if _, err := os.ReadFile(path); !errors.Is(err, os.ErrPermission) {
					t.Skip("this account can read files despite their permission bits")
				}
			}
			if kind != "directory" && kind != "permission denied" {
				if err := os.WriteFile(path, sheet, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			status, err := a.GetSetupRecoveryStatus()
			if err != nil || status.CodesProblem != want || status.CodesPath != path {
				t.Fatalf("%s sheet was not rejected: %+v, %v", kind, status, err)
			}
			// An unavailable sheet must fail before invoking the system document opener.
			if err := a.OpenSetupRecoveryCodes(); err == nil {
				t.Fatal("unavailable recovery sheet reached the document opener")
			}
			unchanged()
			onboardingJobUnlocked(t, a)
		})
	}
}

func TestOnboardingRecoverySheetRequiresSixDistinctSavedCodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codes.txt")
	code := "AAAA-BBBB-CCCC-DDDD-EEEE-FFFF-0000-1111"
	if err := os.WriteFile(path, []byte("[ ] "+code+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Role: roleServer, AdminRecoveryPath: path}
	for i := range cfg.AdminRecoveryHashes {
		cfg.AdminRecoveryHashes[i] = recoveryHash(code)
	}
	if got := adminCodesProblem(cfg); got != "mismatch" {
		t.Fatalf("one code with six duplicate saved hashes was accepted: %q", got)
	}
}

func TestOnboardingRecoverySheetRejectsSymbolicLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks may require elevation")
	}
	sheet, hashes, err := generateAdminRecoveryCodes("fixture.local")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	target := filepath.Join(root, "original-codes.txt")
	if err := os.WriteFile(target, sheet, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "codes.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Role: roleServer, AdminRecoveryPath: link, AdminRecoveryHashes: hashes}
	if got := adminCodesProblem(cfg); got != "unreadable" {
		t.Fatalf("a linked recovery sheet was accepted as a regular export: %q", got)
	}
}

func TestOnboardingRecoveryKeyRejectsNonRegularOrDamagedFiles(t *testing.T) {
	for _, kind := range []string{"empty", "malformed", "directory", "oversized", "link"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "recovery.pem")
			switch kind {
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "link":
				if runtime.GOOS == "windows" {
					t.Skip("creating symlinks may require elevation")
				}
				target := filepath.Join(root, "original.pem")
				if err := os.WriteFile(target, []byte("fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			default:
				data := []byte{}
				switch kind {
				case "malformed":
					data = []byte("not a CARE private key")
				case "oversized":
					data = bytes.Repeat([]byte("x"), 16385)
				}
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cfg := Config{BackupCertificate: "fixture certificate", BackupRecoveryPath: path, BackupRecoveryVerified: true}
			status := setupRecoveryStatus(cfg)
			if status.BackupVerified || status.BackupProblem != "unreadable" || status.BackupPath != path {
				t.Fatalf("%s key did not invalidate verification: %+v", kind, status)
			}
		})
	}
}

func TestOnboardingRecoveryExportActivatesPathOnlyAfterPersistence(t *testing.T) {
	a := roleApp(t)
	a.cfg = Config{Role: roleServer, MDNSName: "fixture.local"}
	previousPath := filepath.Join(t.TempDir(), "previous-codes.txt")
	if err := a.saveAdminRecoveryCodes(previousPath); err != nil {
		t.Fatal(err)
	}
	cfg := a.loadConfig()
	cfg.RecoveryFailures, cfg.RecoveryRetryAfter = 4, 12345
	if err := a.saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	unchanged := onboardingConfigUnchanged(t, a)
	configFile := a.configPath()
	a.configFile = t.TempDir()
	unactivated := filepath.Join(t.TempDir(), "unactivated-codes.txt")
	if err := a.saveAdminRecoveryCodes(unactivated); err == nil {
		t.Fatal("failed settings persistence activated a replacement sheet")
	}
	a.configFile = configFile
	unchanged()
	if _, err := os.Stat(unactivated); err != nil {
		t.Fatalf("fixture did not reach failure after the export: %v", err)
	}
	if got := adminCodesProblem(a.loadConfig()); got != "" {
		t.Fatalf("unsuccessful activation invalidated the previous sheet: %q", got)
	}
	replacement := filepath.Join(t.TempDir(), "replacement-codes.txt")
	if err := a.saveAdminRecoveryCodes(replacement); err != nil {
		t.Fatal(err)
	}
	saved, err := loadConfig(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if saved != a.loadConfig() || saved.AdminRecoveryPath != replacement || saved.AdminRecoveryHashes == cfg.AdminRecoveryHashes ||
		saved.RecoveryFailures != 0 || saved.RecoveryRetryAfter != 0 {
		t.Fatal("successful export did not durably activate its path, hashes and throttle reset")
	}
	if got := adminCodesProblem(saved); got != "" {
		t.Fatalf("persisted recovery path does not verify after reload: %q", got)
	}
	configData, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	sheet, err := os.ReadFile(replacement)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(sheet), "\n") {
		if code, ok := strings.CutPrefix(line, "[ ] "); ok && bytes.Contains(configData, []byte(code)) {
			t.Fatal("recovery export saved a plaintext code in settings")
		}
	}
}

func TestOnboardingPreparedRecoveryPathKeepsUnusedSetupRemovable(t *testing.T) {
	a := roleApp(t)
	a.cfg = Config{Role: roleServer, MDNSName: "fixture.local"}
	path := filepath.Join(t.TempDir(), "codes.txt")
	if err := a.saveAdminRecoveryCodes(path); err != nil {
		t.Fatal(err)
	}
	sheet, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	unchanged := onboardingConfigUnchanged(t, a)
	if installed, err := a.setUp(); err != nil || installed {
		t.Fatalf("a saved recovery-sheet path counted as an installed clinic: %v, %v", installed, err)
	}
	if got := a.removalExitCode(); got != exitRemovable {
		t.Fatalf("unused recovery setup blocked OS-removal eligibility: %d", got)
	}
	unchanged()
	if err := a.ClearRole(); err != nil {
		t.Fatalf("a saved recovery-sheet path prevented changing an unused role: %v", err)
	}
	if a.loadConfig() != (Config{}) {
		t.Fatal("clearing an unused setup retained its role/recovery settings")
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(sheet, after) {
		t.Fatal("clearing an unused setup changed the exported recovery sheet")
	}
	if a.removeTarget != "" || a.closing || a.quitConfirmed.Load() {
		t.Fatal("eligibility checks started application removal or quitting")
	}
}

func TestOnboardingReadGuardFailuresReleaseTheirLock(t *testing.T) {
	for _, state := range []string{"client", "closing"} {
		for _, method := range []string{"validate", "open codes"} {
			t.Run(state+"/"+method, func(t *testing.T) {
				a := roleApp(t)
				cfg := Config{Role: roleServer}
				if state == "client" {
					cfg.Role = roleClient
				}
				if err := a.saveConfig(cfg); err != nil {
					t.Fatal(err)
				}
				a.closing = state == "closing"
				unchanged := onboardingConfigUnchanged(t, a)
				var err error
				if method == "validate" {
					var issues []SetupIssue
					issues, err = a.ValidateSetup("care", settingsPassword, "")
					if len(issues) != 0 {
						t.Fatalf("guard failure returned setup results: %+v", issues)
					}
				} else {
					err = a.OpenSetupRecoveryCodes()
				}
				if err == nil {
					t.Fatal("read bypassed its role/closing guard")
				}
				onboardingJobUnlocked(t, a)
				unchanged()
			})
		}
	}
}

func TestOnboardingValidationDeduplicatesIssuesWithoutWriting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX command fixtures and avoids Windows network-profile checks")
	}
	a := roleApp(t)
	if err := a.saveConfig(Config{Role: roleServer}); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	for _, name := range []string{"docker", "git"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	unchanged := onboardingConfigUnchanged(t, a)
	// The invalid label prevents mDNS queries; failed Docker skips residue scans.
	issues, err := a.ValidateSetup("not a clinic label", "weak", "relative-backups")
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int)
	for _, issue := range issues {
		counts[issue.Step]++
		if strings.TrimSpace(issue.Message) == "" {
			t.Fatalf("issue has no operator-facing explanation: %+v", issue)
		}
	}
	for _, step := range []string{"software", "address", "backup", "admin"} {
		if counts[step] != 1 {
			t.Fatalf("step %s has %d issues instead of one: %+v", step, counts[step], issues)
		}
	}
	onboardingJobUnlocked(t, a)
	unchanged()
	if _, err := os.Stat(a.installDir()); !os.IsNotExist(err) {
		t.Fatalf("validation created installation files: %v", err)
	}
}

func TestOnboardingRunSetupRejectsInstalledOrPartialStateWithoutAWorker(t *testing.T) {
	for _, state := range []string{"installed", "partial", "closing", "reader", "writer"} {
		t.Run(state, func(t *testing.T) {
			a := roleApp(t)
			cfg := Config{Role: roleServer, MDNSName: "care.local", AdminPwHash: "unfinished"}
			if state == "installed" {
				cfg.SetupDone = true
			}
			if err := a.saveConfig(cfg); err != nil {
				t.Fatal(err)
			}
			unchanged := onboardingConfigUnchanged(t, a)
			a.closing = state == "closing"
			switch state {
			case "reader":
				a.jobMu.RLock()
			case "writer":
				a.jobMu.Lock()
			}
			err := a.RunSetup("care", settingsPassword, "")
			switch state {
			case "reader":
				if a.jobMu.TryLock() {
					a.jobMu.Unlock()
					t.Fatal("rejected setup released another reader's lock")
				}
				a.jobMu.RUnlock()
			case "writer":
				if a.jobMu.TryRLock() {
					a.jobMu.RUnlock()
					t.Fatal("rejected setup released another job's lock")
				}
				a.jobMu.Unlock()
			}
			if err == nil {
				t.Fatal("setup was accepted instead of rejected synchronously")
			}
			if state == "installed" || state == "partial" {
				if !strings.HasPrefix(err.Error(), "setup needs attention (cleanup):") {
					t.Fatalf("persisted install state did not fail preflight: %v", err)
				}
			}
			if active := a.activeJob.Load(); active != nil {
				t.Fatalf("rejected setup started an asynchronous worker: %v", active)
			}
			onboardingJobUnlocked(t, a)
			unchanged()
			if _, err := os.Stat(a.installDir()); !os.IsNotExist(err) {
				t.Fatalf("rejected setup created installation files: %v", err)
			}
		})
	}
}

func TestOnboardingFailedSetupWorkerReleasesExclusiveJob(t *testing.T) {
	for _, failure := range []string{"error", "panic"} {
		t.Run(failure, func(t *testing.T) {
			a := roleApp(t)
			if err := a.saveConfig(Config{Role: roleServer}); err != nil {
				t.Fatal(err)
			}
			unchanged := onboardingConfigUnchanged(t, a)
			entered, release := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			a.jobMu.Lock()
			err := a.runLockedJob(func() error {
				close(entered)
				<-release
				if failure == "panic" {
					panic("fixture setup failure")
				}
				return errors.New("fixture setup failure")
			}, true, "setup")
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("worker did not start")
			}
			if active := a.activeJob.Load(); active != "setup" {
				t.Fatalf("worker did not own its active label: %v", active)
			}
			if a.jobMu.TryRLock() {
				a.jobMu.RUnlock()
				t.Fatal("worker did not retain the exclusive lock")
			}
			close(release)
			done := make(chan bool, 1)
			go func() {
				a.jobMu.Lock()
				stopped := a.activeJob.Load() == ""
				a.jobMu.Unlock()
				done <- stopped
			}()
			select {
			case stopped := <-done:
				if !stopped {
					t.Fatal("failed worker retained its active label")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("failed worker retained the exclusive lock")
			}
			unchanged()
		})
	}
}

func TestOnboardingStateIncludesPlatformWithoutServerProbes(t *testing.T) {
	for _, role := range []string{"", roleClient} {
		t.Run(role, func(t *testing.T) {
			a := roleApp(t)
			cfg := Config{Role: role}
			if role == roleClient {
				cfg.ClientURL = "https://clinic.local"
			}
			if err := a.saveConfig(cfg); err != nil {
				t.Fatal(err)
			}
			unchanged := onboardingConfigUnchanged(t, a)
			state, err := a.GetState()
			if err != nil || state.Platform != runtime.GOOS || state.Role != role || state.ClientURL != cfg.ClientURL {
				t.Fatalf("incorrect platform/role state: %+v, %v", state, err)
			}
			data, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			if string(fields["platform"]) != `"`+runtime.GOOS+`"` {
				t.Fatalf("state omitted the bridge platform field: %s", data)
			}
			unchanged()
		})
	}
}

func TestOnboardingCareCheckJSONDistinguishesFailureFromUpToDate(t *testing.T) {
	for _, state := range []CareCheck{
		{},
		{Running: true},
		{Found: true},
		{Error: "couldn't check \"CARE\"\ntry again"},
	} {
		data, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		_, hasError := fields["error"]
		if hasError != (state.Error != "") || fields["running"] == nil || fields["found"] == nil {
			t.Fatalf("CARE check lost its failure/running/found contract: %s", data)
		}
		var decoded CareCheck
		if err := json.Unmarshal(data, &decoded); err != nil || decoded != state {
			t.Fatalf("CARE check failed to round-trip: %+v, %v", decoded, err)
		}
	}
}
