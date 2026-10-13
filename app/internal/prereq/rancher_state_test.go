package prereq

import (
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

func TestVMStuck(t *testing.T) {
	up := func() bool { return true }
	down := func() bool { return false }
	for _, tc := range []struct {
		name    string
		state   string
		known   bool
		windows bool
		distro  func() bool
		want    bool
	}{
		{"error after a WSL update", "ERROR", true, true, down, true},
		{"error on macOS", "ERROR", true, false, up, true},
		{"starting is waited on", "STARTING", true, true, down, false},
		{"stopping is waited on", "STOPPING", true, true, down, false},
		{"started with its VM gone", "STARTED", true, true, down, true},
		{"started and running", "STARTED", true, true, up, false},
		{"no answer and no VM", "", false, true, down, true},
		{"no answer but VM running", "", false, true, up, false},
		{"stopped on macOS", "STOPPED", true, false, up, true},
		{"no answer on macOS", "", false, false, up, false},
	} {
		if got := vmStuck(tc.state, tc.known, tc.windows, tc.distro); got != tc.want {
			t.Errorf("%s: vmStuck = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestParseVMState(t *testing.T) {
	if state, ok := parseVMState([]byte(`{"vmState":"ERROR","locked":false}`)); !ok || state != "ERROR" {
		t.Fatalf("parseVMState = %q, %v", state, ok)
	}
	for _, bad := range []string{``, `404 Not Found`, `{"locked":false}`} {
		if _, ok := parseVMState([]byte(bad)); ok {
			t.Errorf("parseVMState(%q) claimed a state", bad)
		}
	}
}

func TestDistroListedReadsWSLOutput(t *testing.T) {
	utf16 := func(s string) []byte {
		out := []byte{0xff, 0xfe}
		for _, r := range s {
			out = append(out, byte(r), 0)
		}
		return out
	}
	if !distroListed(utf16("rancher-desktop\r\nUbuntu\r\n"), rancherDistro) {
		t.Fatal("running distribution not found in UTF-16 output")
	}
	if distroListed(utf16("rancher-desktop-data\r\n"), rancherDistro) {
		t.Fatal("the data distribution was taken for the VM")
	}
	if distroListed(nil, rancherDistro) {
		t.Fatal("empty output listed a distribution")
	}
}

func TestRancherIsNotLaunchedFromTheInstallFolder(t *testing.T) {
	pr := NewProvisioner(proc.Runner{Dir: `C:\Users\someone\care-clinic\install`}, nil, nil)
	if dir := pr.launcher().Dir; dir == pr.run.Dir || dir == "" {
		t.Fatalf("Rancher would run from %q, keeping the install folder open", dir)
	}
}
