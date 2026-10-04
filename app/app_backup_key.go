package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ohcnetwork/care_desktop/app/internal/backup"
	"golang.org/x/crypto/argon2"
)

// Fixed, versioned parameters prevent a modified settings file from choosing
// excessive KDF work. Each wrapping uses a fresh salt and GCM nonce.
type encryptedBackupKey struct {
	Version    int    `json:"version"`
	Salt       []byte `json:"salt"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

func backupKeyAAD(cfg Config) []byte {
	digest := sha256.Sum256([]byte(cfg.BackupCertificate))
	return []byte(fmt.Sprintf("CARE Clinic backup key v1\x00%s\x00%x", cfg.MDNSName, digest))
}

func backupKeyCipher(password string, salt []byte) (cipher.AEAD, error) {
	passwordBytes := []byte(password)
	defer clear(passwordBytes)
	derived := argon2.IDKey(passwordBytes, salt, 3, 64*1024, 2, 32)
	defer clear(derived)
	block, err := aes.NewCipher(derived)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func encryptBackupKey(cfg Config, password string, key []byte) (string, error) {
	if password == "" {
		return "", errors.New("an admin password is required to protect the local backup key")
	}
	if err := backup.VerifyRecoveryFile([]byte(cfg.BackupCertificate), key); err != nil {
		return "", err
	}
	envelope := encryptedBackupKey{Version: 1, Salt: make([]byte, 16), Nonce: make([]byte, 12)}
	if _, err := rand.Read(envelope.Salt); err != nil {
		return "", err
	}
	if _, err := rand.Read(envelope.Nonce); err != nil {
		return "", err
	}
	aead, err := backupKeyCipher(password, envelope.Salt)
	if err != nil {
		return "", err
	}
	envelope.Ciphertext = aead.Seal(nil, envelope.Nonce, key, backupKeyAAD(cfg))
	data, err := json.Marshal(envelope)
	return string(data), err
}

func decryptBackupKey(cfg Config, password string) ([]byte, error) {
	if cfg.BackupKeyEncrypted == "" || cfg.BackupKeyNeedsEnrollment {
		return nil, errors.New("password-only backup key downloads are not enrolled; select a surviving recovery PEM to enroll this password")
	}
	const problem = "the encrypted backup key could not be unlocked or verified; select a surviving recovery PEM to re-enroll it"
	var envelope encryptedBackupKey
	if len(cfg.BackupKeyEncrypted) > 32*1024 || json.Unmarshal([]byte(cfg.BackupKeyEncrypted), &envelope) != nil ||
		envelope.Version != 1 || len(envelope.Salt) != 16 || len(envelope.Nonce) != 12 ||
		len(envelope.Ciphertext) < 16 || len(envelope.Ciphertext) > 16384+16 {
		return nil, errors.New(problem)
	}
	aead, err := backupKeyCipher(password, envelope.Salt)
	if err != nil {
		return nil, errors.New(problem)
	}
	key, err := aead.Open(nil, envelope.Nonce, envelope.Ciphertext, backupKeyAAD(cfg))
	if err != nil {
		return nil, errors.New(problem)
	}
	if err := backup.VerifyRecoveryFile([]byte(cfg.BackupCertificate), key); err != nil {
		clear(key)
		return nil, errors.New(problem)
	}
	return key, nil
}

func prepareSetupBackupKey(cfg *Config, password string) error {
	key, err := backup.ReadRecoveryFile(cfg.BackupRecoveryPath)
	if err != nil {
		return err
	}
	defer clear(key)
	envelope, err := encryptBackupKey(*cfg, password, key)
	if err != nil {
		return fmt.Errorf("could not protect the local backup recovery key: %w", err)
	}
	cfg.BackupKeyEncrypted = envelope
	cfg.BackupKeyNeedsEnrollment = false
	return nil
}
