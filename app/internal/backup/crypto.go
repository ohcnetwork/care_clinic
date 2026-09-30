package backup

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const certificateName = "backup-cert.pem"

func (s *Store) keysDir() string  { return filepath.Join(s.Dir, "keys") }
func (s *Store) certPath() string { return filepath.Join(s.keysDir(), certificateName) }

func (s *Store) BackupEncryptionOn() bool {
	_, err := os.Stat(s.certPath())
	return err == nil
}

func (s *Store) EnsureKeysDir() error { return os.MkdirAll(s.keysDir(), 0o700) }

// The private key is exported by the UI, never installed beside the backups.
// Standard PEM/X.509 keeps these files compatible with OpenSSL CMS.
func GenerateRecoveryFile() (certificate, recovery []byte, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "care-backup"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(100, 0, 0),
		KeyUsage: x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	certificate = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	recovery = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certificate, recovery, nil
}

func parseCertificate(data []byte) (*x509.Certificate, error) {
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("invalid backup certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("invalid backup certificate: %w", err)
	}
	if key, ok := cert.PublicKey.(*rsa.PublicKey); !ok || key.N.BitLen() < 3072 {
		return nil, fmt.Errorf("invalid backup encryption public key")
	}
	return cert, nil
}

func ReadRecoveryFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("could not open the backup recovery file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 16384 {
		return nil, fmt.Errorf("choose a nonempty CARE backup recovery file, not a link or directory")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	_, err = parseRecoveryFile(data)
	return data, err
}

func parseRecoveryFile(data []byte) (*rsa.PrivateKey, error) {
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "RSA PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("this is not a CARE backup recovery file")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("the backup recovery file is damaged: %w", err)
	}
	if key.N.BitLen() < 3072 {
		return nil, fmt.Errorf("the backup recovery key is too short")
	}
	if err := key.Validate(); err != nil {
		return nil, fmt.Errorf("the backup recovery file is invalid: %w", err)
	}
	return key, nil
}

func VerifyRecoveryFile(certificate, recovery []byte) error {
	cert, err := parseCertificate(certificate)
	if err != nil {
		return err
	}
	key, err := parseRecoveryFile(recovery)
	if err != nil {
		return err
	}
	if !key.PublicKey.Equal(cert.PublicKey) {
		return fmt.Errorf("this recovery file belongs to a different clinic; choose the file you just saved")
	}
	return nil
}

func (s *Store) InstallCertificate(certificate []byte) error {
	if _, err := parseCertificate(certificate); err != nil {
		return err
	}
	foreign, err := s.ForeignRecoveryData()
	if err != nil {
		return err
	}
	if foreign {
		return fmt.Errorf("the backup folder contains another clinic's backups; choose an empty folder")
	}
	if err := s.EnsureKeysDir(); err != nil {
		return err
	}
	if err := writeKeyCopy(s.certPath(), certificate); err != nil {
		return err
	}
	if err := s.CopyBackupCertificate(); err != nil {
		return err
	}
	s.logln("Backup encryption ready; only the public certificate is installed.")
	return nil
}

func (s *Store) CopyBackupCertificate() error {
	data, err := os.ReadFile(s.certPath())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.BackupDir, 0o755); err != nil {
		return err
	}
	return writeKeyCopy(filepath.Join(s.BackupDir, certificateName), data)
}

func (s *Store) PreserveBackupCertificate() error {
	if err := CheckLocation(s.BackupDir, s.Dir); err != nil {
		return err
	}
	if _, err := os.Stat(s.certPath()); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return s.CopyBackupCertificate()
}

func (s *Store) hasEncryptedBackups() (bool, error) {
	entries, err := os.ReadDir(s.BackupDir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if safeName.MatchString(entry.Name()) && strings.HasSuffix(entry.Name(), ".enc") {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) ForeignRecoveryData() (bool, error) {
	encrypted, err := s.hasEncryptedBackups()
	if err != nil {
		return false, err
	}
	existing, err := os.ReadFile(filepath.Join(s.BackupDir, certificateName))
	if os.IsNotExist(err) {
		return encrypted, nil
	}
	if err != nil {
		return false, err
	}
	cert, err := os.ReadFile(s.certPath())
	if os.IsNotExist(err) {
		return true, nil
	}
	return !bytes.Equal(existing, cert), err
}

func (s *Store) DiscardUnusedCertificate() error {
	encrypted, err := s.hasEncryptedBackups()
	if err != nil || encrypted {
		return err
	}
	path := filepath.Join(s.BackupDir, certificateName)
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	cert, err := os.ReadFile(s.certPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !bytes.Equal(existing, cert) {
		return err
	}
	return os.Remove(path)
}

func CheckLocation(dir, protected string) error {
	resolve := func(path string) (string, error) {
		if !filepath.IsAbs(path) {
			return "", fmt.Errorf("expected an absolute directory path: %s", path)
		}
		for current := filepath.Clean(path); ; current = filepath.Dir(current) {
			resolved, err := filepath.EvalSymlinks(current)
			if err == nil {
				rel, err := filepath.Rel(current, path)
				if err != nil {
					return "", err
				}
				return filepath.Join(resolved, rel), nil
			}
			if !os.IsNotExist(err) || filepath.Dir(current) == current {
				return "", err
			}
		}
	}
	target, err := resolve(dir)
	if err != nil {
		return err
	}
	root, err := resolve(protected)
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.VolumeName(target), filepath.VolumeName(root)) {
		return nil
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("choose a location outside %s", protected)
	}
	return nil
}

func (s *Store) DeleteBackups() error {
	entries, err := os.ReadDir(s.BackupDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var failed []error
	kept := false
	for _, entry := range entries {
		name := entry.Name()
		candidate := strings.Replace(strings.TrimPrefix(name, "."), ".tmp", "", 1)
		if entry.IsDir() || (name != certificateName && name != ".backup.lock" && !safeName.MatchString(candidate)) {
			kept = true
			continue
		}
		if err := os.Remove(filepath.Join(s.BackupDir, name)); err != nil {
			failed = append(failed, err)
		}
	}
	if err := errors.Join(failed...); err != nil {
		return err
	}
	if kept {
		s.logln("Unrecognized files were kept in " + s.BackupDir)
		return nil
	}
	return os.Remove(s.BackupDir)
}

func writeKeyCopy(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("the certificate at %s must be a regular file, not a link or directory", path)
		}
		existing, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(existing, b) {
			return fmt.Errorf("a different backup certificate already exists at %s; it was not overwritten", path)
		}
		return nil
	}
	if err != nil {
		return err
	}
	_, writeErr := f.Write(b)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	err = errors.Join(writeErr, f.Close())
	if err != nil {
		return errors.Join(err, os.Remove(path))
	}
	return nil
}
