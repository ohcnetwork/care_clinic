package backup

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func keyStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	s := &Store{Dir: filepath.Join(root, "install"), BackupDir: filepath.Join(root, "backups")}
	if err := os.MkdirAll(s.keysDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.BackupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRecoveryFileAndOpenSSLRoundTrip(t *testing.T) {
	cert, key, err := GenerateRecoveryFile()
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyRecoveryFile(cert, key); err != nil {
		t.Fatal(err)
	}
	otherCert, _, err := GenerateRecoveryFile()
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyRecoveryFile(otherCert, key); err == nil {
		t.Fatal("wrong clinic's key accepted")
	}
	for _, data := range [][]byte{nil, cert, []byte("not a key"), append(key, []byte("extra")...)} {
		if err := VerifyRecoveryFile(cert, data); err == nil {
			t.Fatal("invalid recovery file accepted")
		}
	}
	s := keyStore(t)
	if err := s.InstallCertificate(cert); err != nil {
		t.Fatal(err)
	}
	if err := s.InstallCertificate(cert); err != nil {
		t.Fatal("retry must be idempotent:", err)
	}
	if err := s.InstallCertificate(otherCert); err == nil {
		t.Fatal("installed encryption identity was replaced")
	}
	for _, dir := range []string{s.keysDir(), s.BackupDir} {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 || entries[0].Name() != certificateName {
			t.Fatalf("only a public certificate may be installed: %v, %v", entries, err)
		}
	}
	openssl, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("OpenSSL is supplied by the backup image")
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "recovery.pem")
	input := filepath.Join(dir, "plain")
	encrypted := filepath.Join(dir, "encrypted")
	output := filepath.Join(dir, "decrypted")
	for path, data := range map[string][]byte{keyPath: key, input: []byte("synthetic patient backup")} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ReadRecoveryFile(keyPath); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"cms", "-encrypt", "-binary", "-aes-256-cbc", "-outform", "DER", "-in", input, "-out", encrypted, s.certPath()},
		{"cms", "-decrypt", "-binary", "-inform", "DER", "-in", encrypted, "-out", output, "-inkey", keyPath, "-passin", "pass:"},
	} {
		if out, err := exec.Command(openssl, args...).CombinedOutput(); err != nil {
			t.Fatalf("OpenSSL compatibility: %v: %s", err, out)
		}
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "synthetic patient backup" {
		t.Fatalf("round trip: %q, %v", data, err)
	}
}

func TestCertificateOwnershipAndCleanup(t *testing.T) {
	s := keyStore(t)
	cert := []byte("public certificate")
	if err := os.WriteFile(s.certPath(), cert, 0o600); err != nil {
		t.Fatal(err)
	}
	check := func(want bool) {
		t.Helper()
		got, err := s.ForeignRecoveryData()
		if err != nil || got != want {
			t.Fatalf("foreign=%v, want=%v: %v", got, want, err)
		}
	}
	check(false)
	if err := s.CopyBackupCertificate(); err != nil {
		t.Fatal(err)
	}
	check(false)
	dump := filepath.Join(s.BackupDir, "care-20260101-010101.dump.enc")
	if err := os.WriteFile(dump, []byte("encrypted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.DiscardUnusedCertificate(); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err := os.Remove(s.certPath()); err != nil {
		t.Fatal(err)
	}
	check(true)
	if err := s.DiscardUnusedCertificate(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.certPath(), cert, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dump); err != nil {
		t.Fatal(err)
	}
	if err := s.DiscardUnusedCertificate(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.BackupDir, certificateName)); !os.IsNotExist(err) {
		t.Fatal("unused certificate was not removed")
	}
	if err := os.WriteFile(filepath.Join(s.BackupDir, certificateName), []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.CopyBackupCertificate(); err == nil {
		t.Fatal("foreign certificate overwritten")
	}
	check(true)
}

func TestRecoveryLocationAndLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks may require elevation")
	}
	s := keyStore(t)
	if err := os.WriteFile(s.certPath(), []byte("certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(s.certPath(), filepath.Join(s.BackupDir, certificateName)); err != nil {
		t.Fatal(err)
	}
	if err := s.PreserveBackupCertificate(); err == nil {
		t.Fatal("certificate copy may not link into install")
	}
	if _, err := ReadRecoveryFile(filepath.Join(s.BackupDir, certificateName)); err == nil {
		t.Fatal("recovery file may not be a link")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(s.Dir, alias); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"backups", filepath.Join("missing", "nested", "backups")} {
		if err := CheckLocation(filepath.Join(alias, suffix), s.Dir); err == nil {
			t.Fatal("alias into installation accepted")
		}
	}
}

func TestBackupDeletionKeepsUnrelatedFiles(t *testing.T) {
	s := keyStore(t)
	for _, name := range []string{
		"care-20260101-010101.dump.enc", "files-20260101-010101.tar.gz.enc",
		".care-manual-20260101-010101.dump.tmp.enc", ".backup.lock", certificateName, "notes.txt",
	} {
		if err := os.WriteFile(filepath.Join(s.BackupDir, name), []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteBackups(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(s.BackupDir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "notes.txt" {
		t.Fatalf("unexpected retained files: %v, %v", entries, err)
	}
	data, err := os.ReadFile(filepath.Join(s.BackupDir, "notes.txt"))
	if err != nil || !bytes.Equal(data, []byte("data")) {
		t.Fatal("unrelated file modified")
	}
}
