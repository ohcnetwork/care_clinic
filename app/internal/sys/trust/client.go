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

func FetchClientCertificate(ctx context.Context, clinicURL, at string) (string, error) {
	canonical, err := ClientURL(clinicURL)
	if err != nil {
		return "", err
	}
	host := strings.TrimPrefix(canonical, "https://")
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp4", dialTarget(addr, at))
			},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+host+"/root.crt?ok=1", nil)
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

// dialTarget keeps the clinic's name in the request and TLS handshake while
// sending the packets to an address the caller resolved itself. An empty at
// leaves the operating system's own resolution in charge.
func dialTarget(addr, at string) string {
	if at == "" {
		return addr
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return net.JoinHostPort(at, port)
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

// An empty rootPEM checks OS trust; a supplied root pins the connection before
// installation. An empty at leaves name resolution to the operating system.
func CheckClientConnection(ctx context.Context, clinicURL, rootPEM, at string) error {
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
	conn, err := dialer.DialContext(ctx, "tcp4", dialTarget(net.JoinHostPort(host, "443"), at))
	if err != nil {
		return fmt.Errorf("could not verify the secure connection to %s; check the clinic network and certificate: %w", host, err)
	}
	return conn.Close()
}

// ClientProbe answers "can this computer open the clinic right now?". A clinic
// that does not answer is a result, not a failure: the screen showing this
// polls it while it is open, and a clinic that is switched off overnight is an
// ordinary state rather than something to report as broken.
type ClientProbe struct {
	Reachable bool
	Detail    string
}

// clientProbeTimeout is short on purpose. This runs on a timer behind a visible
// screen, so a clinic that has gone away must not leave a poll in flight until
// the next one starts.
const clientProbeTimeout = 3 * time.Second

// ProbeClient makes one verified HTTPS connection to the clinic and closes it.
// It changes nothing, needs no privileges, does not retry, and resolves the
// clinic the way the browser will. Only a request that cannot be made at all -
// an address that is not a clinic address - is an error.
func ProbeClient(ctx context.Context, clinicURL, rootPEM string) (ClientProbe, error) {
	canonical, err := ClientURL(clinicURL)
	if err != nil {
		return ClientProbe{}, err
	}
	host := strings.TrimPrefix(canonical, "https://")
	config, err := clientTLSConfig(host, rootPEM)
	if err != nil {
		// A pinned root that has expired or been damaged is a state the screen
		// can explain, not a question it asked wrongly.
		return ClientProbe{Detail: "the clinic's security certificate is no longer valid"}, nil
	}
	return probeClient(ctx, config, net.JoinHostPort(host, "443")), nil
}

func probeClient(ctx context.Context, config *tls.Config, address string) ClientProbe {
	ctx, cancel := context.WithTimeout(ctx, clientProbeTimeout)
	defer cancel()
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: clientProbeTimeout}, Config: config}
	conn, err := dialer.DialContext(ctx, "tcp4", address)
	if err != nil {
		return ClientProbe{Detail: probeDetail(err)}
	}
	_ = conn.Close()
	return ClientProbe{Reachable: true}
}

// probeDetail separates "nothing answered" from "something answered but it was
// not the clinic we trust". They need different advice: the first is the clinic
// or the network, the second is the connection itself.
func probeDetail(err error) string {
	var verification *tls.CertificateVerificationError
	var hostname x509.HostnameError
	var authority x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var header tls.RecordHeaderError
	if errors.As(err, &verification) || errors.As(err, &hostname) ||
		errors.As(err, &authority) || errors.As(err, &invalid) || errors.As(err, &header) {
		return "the connection could not be verified"
	}
	return "the server did not answer"
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

// CARoot is one trusted "CARE Clinic Local CA" root held by this computer.
// Store is the keychain or anchor file it lives in, because each platform
// removes a root by a different handle; it is empty on Windows, where the
// fingerprint alone names the certificate.
type CARoot struct {
	Fingerprint string
	Store       string
}

func (r CARoot) String() string {
	if r.Store == "" {
		return r.Fingerprint
	}
	return r.Fingerprint + " in " + r.Store
}

// OtherCARoots lists the trusted CARE clinic roots whose fingerprint is not
// keep. They are left over from an earlier clinic on this computer or from a
// different clinic, and they never belong to the clinic being connected to.
func OtherCARoots(keep string) ([]CARoot, error) {
	return otherCARoots(runtime.GOOS, keep)
}

func otherCARoots(goos, keep string) ([]CARoot, error) {
	switch goos {
	case "darwin":
		var roots []CARoot
		for _, keychain := range []string{darwinLoginKeychain(), darwinSystemKeychain} {
			hashes, err := darwinCARoots(keychain, "")
			if err != nil {
				return nil, err
			}
			for _, fp := range selectRemovable(hashes, keep) {
				roots = append(roots, CARoot{Fingerprint: fp, Store: keychain})
			}
		}
		return roots, nil
	case "windows":
		hashes, err := windowsCARoots()
		if err != nil {
			return nil, err
		}
		var roots []CARoot
		for _, fp := range selectRemovable(hashes, keep) {
			roots = append(roots, CARoot{Fingerprint: fp})
		}
		return roots, nil
	case "linux":
		return linuxCARoots(linuxAnchorDirs, keep)
	}
	return nil, errors.New("client certificate setup is not supported on this operating system")
}

// selectRemovable never returns keep: the certificate being pinned must survive
// a cleanup that runs in the same step as its own installation.
func selectRemovable(found []string, keep string) []string {
	keep = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(keep), " ", ""))
	var out []string
	for _, h := range found {
		h = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(h), " ", ""))
		if h == "" || h == keep {
			continue
		}
		out = appendUnique(out, h)
	}
	return out
}

func windowsCARoots() ([]string, error) {
	out, err := (proc.Runner{}).Capture("powershell", "-NoProfile", "-Command",
		"$ErrorActionPreference = 'Stop'; Get-ChildItem -LiteralPath 'Cert:\\LocalMachine\\Root' | "+
			"Where-Object { $_.Subject -eq "+elevate.PSQuote("CN="+CommonName)+" } | "+
			"ForEach-Object { $_.Thumbprint }")
	if err != nil {
		return nil, fmt.Errorf("could not inspect the clinic certificate: %w", err)
	}
	return hexLines(out), nil
}

func hexLines(out string) []string {
	var found []string
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(ln), " ", ""))
		if ln == "" || strings.Trim(ln, "0123456789ABCDEF") != "" {
			continue
		}
		found = appendUnique(found, ln)
	}
	return found
}

var linuxAnchorDirs = []string{
	"/usr/local/share/ca-certificates",
	"/etc/pki/ca-trust/source/anchors",
}

func linuxCARoots(dirs []string, keep string) ([]CARoot, error) {
	keep = strings.ToUpper(strings.TrimSpace(keep))
	var roots []CARoot
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("could not inspect %s: %w", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".crt") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("could not inspect %s: %w", path, err)
			}
			// Anything that is not a single CARE root belongs to someone else.
			if _, err := clientRoot(string(data), false); err != nil {
				continue
			}
			if fp := SHA1Hex(string(data)); fp != "" && fp != keep {
				roots = append(roots, CARoot{Fingerprint: fp, Store: path})
			}
		}
	}
	return roots, nil
}

// removeOtherRootsSteps splits removal by the approval each store needs. macOS
// refuses trust changes to the login keychain from a root script, so those run
// as the signed-in user; everything else joins the single administrator batch.
func removeOtherRootsSteps(goos string, roots []CARoot) (admin, user []elevate.Step) {
	if len(roots) == 0 {
		return nil, nil
	}
	const what = "remove CARE certificates from clinics this computer no longer uses"
	switch goos {
	case "darwin":
		var login, system []string
		for _, root := range roots {
			cmd := "security delete-certificate -t -Z " + root.Fingerprint + " " + elevate.ShQuote(root.Store)
			if root.Store == darwinSystemKeychain {
				system = append(system, cmd)
			} else {
				login = append(login, cmd)
			}
		}
		if len(system) > 0 {
			admin = append(admin, elevate.Step{What: what, Sh: strings.Join(system, "; ")})
		}
		if len(login) > 0 {
			user = append(user, elevate.Step{What: what, Sh: strings.Join(login, "; ")})
		}
	case "windows":
		var cmds []string
		for _, root := range roots {
			cmds = append(cmds, "Remove-Item -LiteralPath "+
				elevate.PSQuote(`Cert:\LocalMachine\Root\`+root.Fingerprint)+" -ErrorAction Stop")
		}
		admin = append(admin, elevate.Step{What: what, PS: strings.Join(cmds, "; ")})
	case "linux":
		paths := make([]string, 0, len(roots))
		for _, root := range roots {
			paths = append(paths, root.Store)
		}
		admin = append(admin, elevate.Step{What: what, Sh: linuxRemovalScript(paths)})
	}
	return admin, user
}

// ApplyClientTrust makes rootPEM the only CARE clinic root this computer
// trusts. The steps in extra run inside the same administrator approval, so a
// client that also has to repair its hosts file is asked for a password once.
// It returns the roots it removed so the caller can record them in the log.
func ApplyClientTrust(rootPEM string, extra []elevate.Step) ([]CARoot, error) {
	if _, err := clientRoot(rootPEM, true); err != nil {
		return nil, err
	}
	present, err := ClientCertificatePresent(rootPEM)
	if err != nil {
		return nil, err
	}
	stale, err := OtherCARoots(SHA1Hex(rootPEM))
	if err != nil {
		return nil, err
	}
	adminRemove, userRemove := removeOtherRootsSteps(runtime.GOOS, stale)
	admin := append(append([]elevate.Step{}, extra...), adminRemove...)
	user := userRemove
	if !present {
		path, cleanup, err := writeTempCertificate(rootPEM)
		defer cleanup()
		if err != nil {
			return nil, err
		}
		step, err := clientCertificateStep(runtime.GOOS, rootPEM, path, false)
		if err != nil {
			return nil, err
		}
		if runtime.GOOS == "darwin" {
			user = append(user, step)
		} else {
			admin = append(admin, step)
		}
	}
	if len(admin) > 0 {
		if err := elevate.Steps(admin); err != nil {
			return nil, fmt.Errorf("could not install the clinic certificate; approve the administrator prompt and try again: %w", err)
		}
	}
	// A keychain that refuses one deletion must not abandon the installation;
	// the survivors are reported by the caller's next inspection instead. One
	// shell for all of it, with the installation last so its exit status is the
	// one that survives - and so macOS is asked for the trust-settings right
	// once for the whole sequence rather than once per command.
	var userErr error
	if len(user) > 0 {
		scripts := make([]string, 0, len(user))
		for _, step := range user {
			scripts = append(scripts, step.Sh)
		}
		userErr = elevate.Run(strings.Join(scripts, "\n"), false)
	}
	present, err = ClientCertificatePresent(rootPEM)
	if err != nil {
		return stale, err
	}
	if !present {
		if userErr != nil {
			return stale, fmt.Errorf("could not install the clinic certificate; approve the request when your computer asks and try again: %w", userErr)
		}
		return stale, errors.New("the clinic certificate was not installed; ask the computer administrator for help")
	}
	return stale, nil
}

func writeTempCertificate(rootPEM string) (string, func(), error) {
	noop := func() {}
	f, err := os.CreateTemp("", "care-client-*.crt")
	if err != nil {
		return "", noop, err
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	if _, err := f.WriteString(rootPEM); err != nil {
		_ = f.Close()
		return "", cleanup, err
	}
	if err := f.Close(); err != nil {
		return "", cleanup, err
	}
	return f.Name(), cleanup, nil
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
