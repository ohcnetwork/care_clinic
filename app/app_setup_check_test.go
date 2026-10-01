package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/backup"
)

func TestSetupRecoveryStatusChecksExportedFiles(t *testing.T) {
	a := roleApp(t)
	certificate, key, err := backup.GenerateRecoveryFile()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "recovery.pem")
	if err := saveRecoveryFile(path, key); err != nil {
		t.Fatal(err)
	}
	a.cfg = Config{Role: roleServer, MDNSName: "care.local", BackupCertificate: string(certificate),
		BackupRecoveryPath: path, BackupRecoveryVerified: true}
	codes := filepath.Join(t.TempDir(), "codes.txt")
	if err := a.saveAdminRecoveryCodes(codes); err != nil {
		t.Fatal(err)
	}
	status, err := a.GetSetupRecoveryStatus()
	if err != nil || !status.BackupVerified || !status.CodesSaved ||
		status.BackupProblem != "" || status.CodesProblem != "" ||
		status.BackupPath != path || status.CodesPath != codes {
		t.Fatalf("unexpected saved state: %+v, %v", status, err)
	}
	before := a.loadConfig()
	if err := os.Remove(codes); err != nil {
		t.Fatal(err)
	}
	status, err = a.GetSetupRecoveryStatus()
	if err != nil || status.CodesProblem != "missing" {
		t.Fatalf("missing codes were accepted: %+v, %v", status, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	status, err = a.GetSetupRecoveryStatus()
	if err != nil || status.BackupVerified || status.BackupProblem != "missing" {
		t.Fatalf("missing private key was accepted: %+v, %v", status, err)
	}
	if a.loadConfig() != before {
		t.Fatal("a read-only recovery check changed settings")
	}
}

func TestSetupRecoveryStatusRejectsChangedFiles(t *testing.T) {
	a := roleApp(t)
	certificate, _, err := backup.GenerateRecoveryFile()
	if err != nil {
		t.Fatal(err)
	}
	_, differentKey, err := backup.GenerateRecoveryFile()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "wrong.pem")
	if err := saveRecoveryFile(path, differentKey); err != nil {
		t.Fatal(err)
	}
	a.cfg = Config{Role: roleServer, MDNSName: "care.local", BackupCertificate: string(certificate),
		BackupRecoveryPath: path, BackupRecoveryVerified: true}
	codes := filepath.Join(t.TempDir(), "codes.txt")
	if err := a.saveAdminRecoveryCodes(codes); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codes, []byte("not the exported recovery codes"), 0o600); err != nil {
		t.Fatal(err)
	}
	status := setupRecoveryStatus(a.loadConfig())
	if status.BackupVerified || status.BackupProblem != "mismatch" || status.CodesProblem != "mismatch" {
		t.Fatalf("modified recovery material passed: %+v", status)
	}
}

func TestSetupValidationRespectsRoleAndJobGuards(t *testing.T) {
	for _, role := range []string{"", roleClient} {
		a := roleApp(t)
		a.cfg.Role = role
		if _, err := a.ValidateSetup("care", "PreviewPassword123", ""); err == nil {
			t.Fatalf("role %q could validate server setup", role)
		}
		if _, err := a.ReplaceSetupBackupRecovery(""); err == nil {
			t.Fatalf("role %q reached the recovery save dialog", role)
		}
		if err := a.OpenSetupRecoveryCodes(); err == nil {
			t.Fatalf("role %q could open recovery materials", role)
		}
	}
	a := roleApp(t)
	a.cfg = Config{Role: roleServer}
	a.jobMu.Lock()
	if _, err := a.ValidateSetup("care", "PreviewPassword123", ""); err == nil {
		t.Fatal("validation ignored a running job")
	}
	a.jobMu.Unlock()
}

func TestRunSetupRejectsPreflightBeforeStartingWorker(t *testing.T) {
	a := roleApp(t)
	a.cfg = Config{Role: roleServer, MDNSName: "care.local", Removing: true}
	before := a.loadConfig()
	issues, err := a.ValidateSetup("care", "PreviewPassword123", "")
	if err != nil || len(issues) != 1 || issues[0].Step != "cleanup" {
		t.Fatalf("unexpected preflight result: %+v, %v", issues, err)
	}
	err = a.RunSetup("care", "PreviewPassword123", "")
	if err == nil || !strings.HasPrefix(err.Error(), "setup needs attention (cleanup):") {
		t.Fatalf("preflight wasn't rejected synchronously: %v", err)
	}
	if a.loadConfig() != before {
		t.Fatal("rejected setup changed the settings")
	}
	if !a.jobMu.TryLock() {
		t.Fatal("rejected setup kept the job lock")
	}
	a.jobMu.Unlock()
}

func TestRecoveryReplacementAndPrintingStayLockedAfterInstallationStarts(t *testing.T) {
	a := roleApp(t)
	a.cfg = Config{Role: roleServer, AdminPwHash: "already-started"}
	if _, err := a.ReplaceSetupBackupRecovery(""); err == nil {
		t.Fatal("partial installation reached the save dialog")
	}
	if err := a.OpenSetupRecoveryCodes(); err == nil {
		t.Fatal("partial installation could open recovery codes without authentication")
	}
}
