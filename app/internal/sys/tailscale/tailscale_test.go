package tailscale

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

func TestRootProxy(t *testing.T) {
	const host = "care.tail1234.ts.net"
	cases := []struct {
		name string
		out  string
		want string
	}{
		{"empty", "", ""},
		{"no config", "{}", ""},
		{"ours", `{"TCP":{"443":{"HTTPS":true}},"Web":{"care.tail1234.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:8480"}}}}}`, "http://127.0.0.1:8480"},
		{"other port", `{"Web":{"care.tail1234.ts.net:8443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:3000"}}}}}`, ""},
		{"sub path only", `{"Web":{"care.tail1234.ts.net:443":{"Handlers":{"/grafana":{"Proxy":"http://127.0.0.1:3000"}}}}}`, ""},
		{"someone else", `{"Web":{"CARE.tail1234.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:3000"}}}}}`, "http://127.0.0.1:3000"},
		{"files", `{"Web":{"care.tail1234.ts.net:443":{"Handlers":{"/":{"Path":"/srv/share"}}}}}`, "path:/srv/share"},
		{"text", `{"Web":{"care.tail1234.ts.net:443":{"Handlers":{"/":{"Text":"hello"}}}}}`, "text"},
	}
	for _, tc := range cases {
		got, err := rootProxy(tc.out, host, 443)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	if got, _ := rootProxy(`{"Web":{"care.tail1234.ts.net:8443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:8480"}}}}}`, host, Port); got != "http://127.0.0.1:8480" {
		t.Errorf("port %d: got %q", Port, got)
	}
	if _, err := rootProxy("not json", host, 443); err == nil {
		t.Error("expected an error for unreadable serve status")
	}
}

func TestCheckWithoutTailscale(t *testing.T) {
	previous := findCLI
	findCLI = func() string { return "" }
	defer func() { findCLI = previous }()
	status := Check(proc.Runner{}, Target(8480))
	if status.Installed || status.Running || status.Enabled || status.Detail == "" {
		t.Errorf("unexpected status %+v", status)
	}
	if err := Disable(proc.Runner{}, Target(8480)); err != nil {
		t.Errorf("disable without Tailscale: %v", err)
	}
	if err := Enable(proc.Runner{}, Target(8480)); err == nil {
		t.Error("enable without Tailscale should fail")
	}
}

func TestSignInDetail(t *testing.T) {
	for _, state := range []string{"NeedsLogin", "NeedsMachineAuth", "Stopped", "Starting", ""} {
		if signInDetail(state) == "" {
			t.Errorf("no detail for %q", state)
		}
	}
}

func fakeCLI(t *testing.T, serveStatus string) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake Tailscale command is a shell script")
	}
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	script := filepath.Join(dir, "tailscale")
	body := "#!/bin/sh\necho \"$*\" >> " + calls + "\n" +
		"if [ \"$1\" = status ]; then echo '{\"BackendState\":\"Running\",\"CertDomains\":[\"care.tail1234.ts.net\"],\"Self\":{\"DNSName\":\"care.tail1234.ts.net.\",\"TailscaleIPs\":[\"100.64.0.1\"]}}'; exit 0; fi\n" +
		"if [ \"$1 $2\" = \"serve status\" ]; then " + serveStatus + "; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	previous := findCLI
	findCLI = func() string { return script }
	t.Cleanup(func() { findCLI = previous })
	return script, calls
}

func TestUnreadableServeStatusBlocksEnable(t *testing.T) {
	_, calls := fakeCLI(t, "echo broken >&2; exit 1")
	status := Check(proc.Runner{}, Target(8480))
	if !status.Unknown || status.Enabled || status.Conflict || status.Detail == "" {
		t.Fatalf("unexpected status %+v", status)
	}
	if err := Enable(proc.Runner{}, Target(8480)); err == nil {
		t.Fatal("enable should refuse while the serve configuration is unknown")
	}
	log, _ := os.ReadFile(calls)
	if strings.Contains(string(log), "--bg") {
		t.Fatalf("serve configuration was changed: %s", log)
	}
}

func TestExistingFileHandlerIsAConflict(t *testing.T) {
	_, calls := fakeCLI(t, `echo '{"Web":{"care.tail1234.ts.net:8443":{"Handlers":{"/":{"Path":"/srv/share"}}}}}'; exit 0`)
	status := Check(proc.Runner{}, Target(8480))
	if !status.Conflict || status.Enabled {
		t.Fatalf("unexpected status %+v", status)
	}
	if err := Enable(proc.Runner{}, Target(8480)); err == nil {
		t.Fatal("enable should refuse to replace another handler")
	}
	log, _ := os.ReadFile(calls)
	if strings.Contains(string(log), "--bg") {
		t.Fatalf("serve configuration was changed: %s", log)
	}
}

func TestEnableServesWhenFree(t *testing.T) {
	_, calls := fakeCLI(t, "echo '{}'; exit 0")
	if err := Enable(proc.Runner{}, Target(8480)); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(calls)
	if !strings.Contains(string(log), "serve --bg --yes --https=8443 http://127.0.0.1:8480") {
		t.Fatalf("serve was not turned on: %s", log)
	}
}
