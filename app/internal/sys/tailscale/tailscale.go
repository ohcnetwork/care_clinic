package tailscale

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

const timeout = 20 * time.Second

const Port = 8443

var legacyPorts = []int{443}

type Status struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	HTTPS     bool   `json:"https"`
	Host      string `json:"host"`
	URL       string `json:"url"`
	IP        string `json:"ip"`
	Enabled   bool   `json:"enabled"`
	Conflict  bool   `json:"conflict"`
	Unknown   bool   `json:"unknown"`
	Detail    string `json:"detail"`
}

type backendStatus struct {
	BackendState string
	CertDomains  []string
	Self         *struct {
		DNSName      string
		TailscaleIPs []string
	}
}

type serveConfig struct {
	Web map[string]struct {
		Handlers map[string]struct {
			Proxy string
			Path  string
			Text  string
		}
	}
}

var findCLI = func() string {
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{"/Applications/Tailscale.app/Contents/MacOS/Tailscale"}
	case "windows":
		for _, env := range []string{"ProgramFiles", "ProgramW6432"} {
			if dir := os.Getenv(env); dir != "" {
				candidates = append(candidates, filepath.Join(dir, "Tailscale", "tailscale.exe"))
			}
		}
	}
	for _, path := range candidates {
		if proc.FileExists(path) {
			return path
		}
	}
	if proc.Exists("tailscale") {
		return "tailscale"
	}
	return ""
}

var cliEnv = []string{"TAILSCALE_BE_CLI=1"}

func capture(run proc.Runner, cli string, args ...string) (string, error) {
	out, err := run.CaptureIn(timeout, cliEnv, cli, args...)
	if err == nil {
		return out, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if detail := strings.TrimSpace(string(exit.Stderr)); detail != "" {
			return out, fmt.Errorf("tailscale %s: %s", args[0], detail)
		}
	}
	return out, fmt.Errorf("tailscale %s: %w", args[0], err)
}

func Target(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func Check(run proc.Runner, target string) Status {
	cli := findCLI()
	if cli == "" {
		return Status{Detail: "Tailscale is not installed on this computer."}
	}
	status := Status{Installed: true}
	out, err := capture(run, cli, "status", "--json")
	if out == "" && err != nil {
		status.Detail = "Couldn't read Tailscale's status. Open Tailscale and check that it is running."
		return status
	}
	var backend backendStatus
	if err := json.Unmarshal([]byte(out), &backend); err != nil {
		status.Detail = "Couldn't read Tailscale's status. Open Tailscale and check that it is running."
		return status
	}
	if backend.BackendState != "Running" || backend.Self == nil {
		status.Detail = signInDetail(backend.BackendState)
		return status
	}
	status.Running = true
	status.Host = strings.TrimSuffix(backend.Self.DNSName, ".")
	if status.Host != "" {
		status.URL = fmt.Sprintf("https://%s:%d/", status.Host, Port)
	}
	for _, ip := range backend.Self.TailscaleIPs {
		if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() != nil {
			status.IP = ip
			break
		}
	}
	for _, domain := range backend.CertDomains {
		if strings.EqualFold(strings.TrimSuffix(domain, "."), status.Host) {
			status.HTTPS = true
		}
	}
	if status.Host == "" {
		status.Detail = "Turn on MagicDNS in the Tailscale admin console."
		return status
	}
	if !status.HTTPS {
		status.Detail = "Turn on HTTPS certificates in the Tailscale admin console."
	}
	proxy, err := servedAt(run, cli, status.Host, Port)
	if err != nil {
		status.Unknown = true
		status.Detail = "Couldn't read Tailscale's sharing settings. Open Tailscale and check that it is running."
		return status
	}
	status.Enabled = proxy == target
	status.Conflict = proxy != "" && proxy != target
	if status.Conflict {
		status.Detail = "Tailscale on this computer already shares something else at this address."
	}
	return status
}

func signInDetail(state string) string {
	switch state {
	case "NeedsLogin":
		return "Open Tailscale and sign in with the clinic's account."
	case "NeedsMachineAuth":
		return "Approve this computer in the Tailscale admin console."
	case "Stopped":
		return "Open Tailscale and connect."
	}
	return "Open Tailscale and check that it is connected."
}

func servedAt(run proc.Runner, cli, host string, port int) (string, error) {
	out, err := capture(run, cli, "serve", "status", "--json")
	if err != nil {
		return "", err
	}
	return rootProxy(out, host, port)
}

func rootProxy(out, host string, port int) (string, error) {
	if strings.TrimSpace(out) == "" {
		return "", nil
	}
	var config serveConfig
	if err := json.Unmarshal([]byte(out), &config); err != nil {
		return "", fmt.Errorf("tailscale serve status: %w", err)
	}
	for key, web := range config.Web {
		if !strings.EqualFold(key, fmt.Sprintf("%s:%d", host, port)) {
			continue
		}
		if handler, ok := web.Handlers["/"]; ok {
			switch {
			case handler.Proxy != "":
				return handler.Proxy, nil
			case handler.Path != "":
				return "path:" + handler.Path, nil
			}
			return "text", nil
		}
	}
	return "", nil
}

func Enable(run proc.Runner, target string) error {
	status := Check(run, target)
	switch {
	case !status.Installed, !status.Running, !status.HTTPS, status.Unknown, status.Conflict:
		return errors.New(status.Detail)
	case status.Enabled:
		return nil
	}
	cli := findCLI()
	if err := removeLegacy(run, cli, status.Host, target); err != nil {
		return err
	}
	_, err := capture(run, cli, "serve", "--bg", "--yes", fmt.Sprintf("--https=%d", Port), target)
	return err
}

func removeLegacy(run proc.Runner, cli, host, target string) error {
	for _, port := range legacyPorts {
		if err := turnOff(run, cli, host, port, target); err != nil {
			return err
		}
	}
	return nil
}

func turnOff(run proc.Runner, cli, host string, port int, target string) error {
	proxy, err := servedAt(run, cli, host, port)
	if err != nil || proxy != target {
		return err
	}
	_, err = capture(run, cli, "serve", "--yes", fmt.Sprintf("--https=%d", port), "off")
	return err
}

func Disable(run proc.Runner, target string) error {
	cli := findCLI()
	if cli == "" {
		return nil
	}
	out, err := capture(run, cli, "status", "--json")
	if err != nil {
		return err
	}
	var backend backendStatus
	if err := json.Unmarshal([]byte(out), &backend); err != nil {
		return fmt.Errorf("tailscale status: %w", err)
	}
	if backend.Self == nil {
		return nil
	}
	host := strings.TrimSuffix(backend.Self.DNSName, ".")
	if err := turnOff(run, cli, host, Port, target); err != nil {
		return err
	}
	return removeLegacy(run, cli, host, target)
}
