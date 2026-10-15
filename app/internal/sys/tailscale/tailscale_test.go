package tailscale

import (
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
