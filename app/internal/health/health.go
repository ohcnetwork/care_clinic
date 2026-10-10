package health

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
	"github.com/ohcnetwork/care_desktop/app/internal/vm"
)

type Health struct {
	Active bool   `json:"active"`
	Code   int    `json:"code"`
	Detail string `json:"detail"`
}

func Ping() Health {
	client := &http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	resp, err := client.Get("https://localhost/ping/")
	if err != nil {
		return Health{Active: false, Code: 0, Detail: "nothing answering on :443"}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == 200 {
		return Health{Active: true, Code: 200, Detail: "healthy"}
	}
	return Health{Active: false, Code: resp.StatusCode, Detail: "HTTP " + strings.TrimSpace(resp.Status)}
}

func Wait(log func(string), timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	last := Health{Detail: "nothing answering yet"}
	for n := 1; ; n++ {
		if last = Ping(); last.Active {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("CARE did not become healthy within %s (%s)", timeout, last.Detail)
		}
		logln(log, fmt.Sprintf("  waiting for CARE to answer... (%d) - %s", n, last.Detail))
		time.Sleep(3 * time.Second)
	}
}

func EnsurePortFree(run proc.Runner, host string) error {
	if caddyRunning(run) {
		return nil // the listeners on :80/:443 are our own caddy
	}
	var enginePorts []int
	if vm.Supported() {
		status := vm.Status(context.Background())
		if status.Running {
			enginePorts = status.WebPorts
		}
	}
	for _, port := range []int{80, 443} {
		if slices.Contains(enginePorts, port) {
			continue
		}
		if !tcpBusy(fmt.Sprintf("127.0.0.1:%d", port)) {
			continue
		}
		who := portOccupant(port)
		return fmt.Errorf("port %d is already in use%s.\n"+
			"CARE serves the clinic app on https://%s.local/ (ports 80 and 443). "+
			"Quit whatever is using port %d, then try again.", port, who, strings.TrimSuffix(host, ".local"), port)
	}
	return nil
}

func caddyRunning(run proc.Runner) bool {
	out, err := run.Capture("docker", "compose", "ps", "--services", "--filter", "status=running")
	if err != nil {
		return false
	}
	for _, s := range strings.Split(out, "\n") {
		if strings.TrimSpace(s) == "caddy" {
			return true
		}
	}
	return false
}

func tcpBusy(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 700*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func portOccupant(port int) string {
	var name string
	switch runtime.GOOS {
	case "darwin", "linux":
		out, err := proc.Command("lsof", "-nP", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN", "-F", "c").Output()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if strings.HasPrefix(line, "c") { // "c<command>" field
					name = strings.TrimPrefix(line, "c")
					break
				}
			}
		}
	case "windows":
	}
	if name == "" {
		return ""
	}
	return " by '" + name + "'"
}

func logln(log func(string), s string) {
	if log != nil {
		log(s)
	}
}
