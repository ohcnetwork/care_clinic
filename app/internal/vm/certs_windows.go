package vm

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// ensureCerts creates the self-signed certificate both ends of the Windows TCP
// channel use: the guest agent as server and the docker CLI and this package
// as client. The same file is the CA, so only this install can connect.
// It returns the combined cert+key PEM handed to the guest through fw_cfg.
func ensureCerts(dir string) (string, error) {
	combined := filepath.Join(dir, "guest.pem")
	if _, err := os.Stat(combined); err == nil {
		return combined, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return "", err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "care-engine"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(20, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"care-engine", "localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", err
	}
	kder, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder})
	for name, data := range map[string][]byte{"ca.pem": certPEM, "cert.pem": certPEM, "key.pem": keyPEM} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			return "", err
		}
	}
	return combined, os.WriteFile(combined, append(append([]byte{}, certPEM...), keyPEM...), 0o600)
}
