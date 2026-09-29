package trust

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/mdns"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

func ClientURL(address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", errors.New("enter the clinic address shown on the clinic's main computer")
	}
	if !strings.Contains(address, "://") {
		address = "https://" + address
	}
	u, err := url.Parse(address)
	if err != nil {
		return "", errors.New("enter a clinic address such as care.local")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.User != nil ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(address, "#") ||
		(u.Path != "" && u.Path != "/") || u.RawPath != "" || strings.Contains(u.Host, ":") {
		return "", errors.New("enter just the clinic address, without a port, sign-in details or page path")
	}
	host := strings.ToLower(u.Hostname())
	label := strings.TrimSuffix(host, ".local")
	if label == "" || strings.Contains(label, ".") || mdns.ValidateLabel(label) != nil ||
		strings.Trim(label, ".") != label {
		return "", errors.New("use the clinic's local address, for example care.local")
	}
	return "https://" + label + ".local", nil
}

func FetchClientCertificate(ctx context.Context, clinicURL string) (string, error) {
	canonical, err := ClientURL(clinicURL)
	if err != nil {
		return "", err
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp4", addr)
			},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+strings.TrimPrefix(canonical, "https://")+"/root.crt?ok=1", nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not reach the clinic; check its address, network and main computer: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	return readClientCertificate(response)
}

func readClientCertificate(response *http.Response) (string, error) {
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the clinic could not provide its security certificate (HTTP %d); ask the clinic administrator to check CARE", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if err != nil {
		return "", fmt.Errorf("could not download the clinic certificate: %w", err)
	}
	if len(data) > 64*1024 {
		return "", errors.New("the clinic returned an oversized certificate; nothing was installed")
	}
	if _, err := clientRoot(string(data), true); err != nil {
		return "", err
	}
	return string(data), nil
}

func clientRoot(rootPEM string, checkValidity bool) (*x509.Certificate, error) {
	data := bytes.TrimSpace([]byte(rootPEM))
	block, rest := pem.Decode(data)
	if !bytes.HasPrefix(data, []byte("-----BEGIN CERTIFICATE-----")) ||
		block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("the clinic did not provide a single valid security certificate; nothing was installed")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("could not read the clinic certificate: %w", err)
	}
	if !cert.IsCA || !cert.BasicConstraintsValid || cert.KeyUsage&x509.KeyUsageCertSign == 0 ||
		cert.Subject.CommonName != CommonName || cert.CheckSignatureFrom(cert) != nil {
		return nil, errors.New("this is not a CARE clinic root certificate; nothing was installed")
	}
	if checkValidity && (time.Now().Before(cert.NotBefore) || time.Now().After(cert.NotAfter)) {
		return nil, errors.New("the clinic certificate is not valid now; check this computer's clock or ask the clinic administrator")
	}
	return cert, nil
}

// An empty rootPEM checks OS trust; a supplied root pins the connection before installation.
func CheckClientConnection(ctx context.Context, clinicURL, rootPEM string) error {
	canonical, err := ClientURL(clinicURL)
	if err != nil {
		return err
	}
	host := strings.TrimPrefix(canonical, "https://")
	config, err := clientTLSConfig(host, rootPEM)
	if err != nil {
		return err
	}
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 8 * time.Second}, Config: config}
	conn, err := dialer.DialContext(ctx, "tcp4", net.JoinHostPort(host, "443"))
	if err != nil {
		return fmt.Errorf("could not verify the secure connection to %s; check the clinic network and certificate: %w", host, err)
	}
	return conn.Close()
}

func clientTLSConfig(host, rootPEM string) (*tls.Config, error) {
	config := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if rootPEM != "" {
		cert, err := clientRoot(rootPEM, true)
		if err != nil {
			return nil, err
		}
		config.RootCAs = x509.NewCertPool()
		config.RootCAs.AddCert(cert)
	} else if runtime.GOOS == "linux" {
		config.RootCAs = rootsFromFiles(linuxTrustBundles)
	}
	return config, nil
}

func ClientCertificatePresent(rootPEM string) (bool, error) {
	cert, err := clientRoot(rootPEM, false)
	if err != nil {
		return false, err
	}
	switch runtime.GOOS {
	case "darwin":
		present, err := certInKeychain(darwinLoginKeychain(), SHA1Hex(rootPEM))
		if err != nil || !present {
			return false, err
		}
		_, err = cert.Verify(x509.VerifyOptions{})
		return err == nil, nil
	case "windows":
		out, err := (proc.Runner{}).Capture("powershell", "-NoProfile", "-Command",
			"$ErrorActionPreference = 'Stop'; Test-Path -LiteralPath "+
				elevate.PSQuote(`Cert:\LocalMachine\Root\`+SHA1Hex(rootPEM)))
		if err != nil {
			return false, fmt.Errorf("could not inspect the clinic certificate: %w", err)
		}
		switch strings.TrimSpace(out) {
		case "True":
			return true, nil
		case "False":
			return false, nil
		default:
			return false, errors.New("Windows returned an unknown certificate state")
		}
	case "linux":
		present := false
		for _, path := range clientAnchors(cert) {
			data, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return false, fmt.Errorf("could not inspect the clinic certificate: %w", err)
			}
			existing, err := clientRoot(string(data), false)
			if err != nil || !bytes.Equal(existing.Raw, cert.Raw) {
				return false, fmt.Errorf("the certificate at %s was changed; ask an administrator to check it", path)
			}
			present = true
		}
		return present, nil
	default:
		return false, errors.New("client certificate setup is not supported on this operating system")
	}
}

func clientAnchors(cert *x509.Certificate) []string {
	name := fmt.Sprintf("care-client-%x.crt", sha256.Sum256(cert.Raw))
	return []string{
		filepath.Join("/usr/local/share/ca-certificates", name),
		filepath.Join("/etc/pki/ca-trust/source/anchors", name),
	}
}

func clientCertificateStep(goos, rootPEM, path string, remove bool) (elevate.Step, error) {
	cert, err := clientRoot(rootPEM, !remove)
	if err != nil {
		return elevate.Step{}, err
	}
	step := elevate.Step{What: "set up this computer's access to CARE"}
	switch goos {
	case "darwin":
		step.Sh = "security add-trusted-cert -r trustRoot -k " +
			elevate.ShQuote(darwinLoginKeychain()) + " " + elevate.ShQuote(path)
		if remove {
			step.Sh = "security delete-certificate -t -Z " + SHA1Hex(rootPEM) + " " + elevate.ShQuote(darwinLoginKeychain())
		}
	case "windows":
		step.PS = installPS(path)
		if remove {
			step.PS = "Remove-Item -LiteralPath " + elevate.PSQuote(`Cert:\LocalMachine\Root\`+SHA1Hex(rootPEM)) + " -ErrorAction Stop"
		}
	case "linux":
		anchors := clientAnchors(cert)
		step.Sh = "set -e\nif command -v update-ca-certificates >/dev/null 2>&1; then\n" +
			"cp " + elevate.ShQuote(path) + " " + elevate.ShQuote(anchors[0]) +
			"\nchmod 644 " + elevate.ShQuote(anchors[0]) + "\nupdate-ca-certificates\n" +
			"elif command -v update-ca-trust >/dev/null 2>&1; then\n" +
			"cp " + elevate.ShQuote(path) + " " + elevate.ShQuote(anchors[1]) +
			"\nchmod 644 " + elevate.ShQuote(anchors[1]) + "\nupdate-ca-trust\n" +
			"else echo 'No supported system certificate store was found.' >&2; exit 1; fi"
		if remove {
			step.Sh = linuxRemovalScript(anchors)
		}
	default:
		return elevate.Step{}, errors.New("client certificate setup is not supported on this operating system")
	}
	return step, nil
}

func runClientStep(goos string, step elevate.Step) error {
	if goos == "darwin" {
		return elevate.Run(step.Sh, false)
	}
	return elevate.Steps([]elevate.Step{step})
}

func InstallClientCertificate(rootPEM string) error {
	if _, err := clientRoot(rootPEM, true); err != nil {
		return err
	}
	f, err := os.CreateTemp("", "care-client-*.crt")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.WriteString(rootPEM); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	step, err := clientCertificateStep(runtime.GOOS, rootPEM, f.Name(), false)
	if err != nil {
		return err
	}
	if err := runClientStep(runtime.GOOS, step); err != nil {
		return fmt.Errorf("could not install the clinic certificate; approve the administrator prompt and try again: %w", err)
	}
	present, err := ClientCertificatePresent(rootPEM)
	if err != nil {
		return err
	}
	if !present {
		return errors.New("the clinic certificate was not installed; ask the computer administrator for help")
	}
	return nil
}

func RemoveClientCertificate(rootPEM string) error {
	present, err := ClientCertificatePresent(rootPEM)
	if err != nil || !present {
		return err
	}
	step, err := clientCertificateStep(runtime.GOOS, rootPEM, "", true)
	if err != nil {
		return err
	}
	if err := runClientStep(runtime.GOOS, step); err != nil {
		return fmt.Errorf("could not remove the clinic certificate; approve the administrator prompt and try again: %w", err)
	}
	present, err = ClientCertificatePresent(rootPEM)
	if err != nil {
		return err
	}
	if present {
		return errors.New("the clinic certificate is still installed; removal is incomplete")
	}
	return nil
}
