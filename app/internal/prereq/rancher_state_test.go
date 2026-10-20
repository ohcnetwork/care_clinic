package prereq

import (
	"testing"
	"time"

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
		silent  time.Duration
		docker  time.Duration
		want    bool
	}{
		{"error after a WSL update", "ERROR", true, true, down, 0, 0, true},
		{"error on macOS", "ERROR", true, false, up, 0, 0, true},
		{"starting is waited on", "STARTING", true, true, down, 0, 0, false},
		{"stopping is waited on", "STOPPING", true, true, down, 0, 0, false},
		{"started with its VM gone", "STARTED", true, true, down, 0, 0, true},
		{"started and running", "STARTED", true, true, up, 0, 0, false},
		{"no answer yet while starting", "", false, true, down, 10 * time.Second, 0, false},
		{"stopped on Windows", "STOPPED", true, true, down, 0, 0, true},
		{"no answer but VM running", "", false, true, up, time.Hour, 0, false},
		{"stopped on macOS", "STOPPED", true, false, up, 0, 0, true},
		{"no answer on macOS", "", false, false, up, time.Hour, 0, false},
		{"silent past the grace with its VM gone", "", false, true, down, rancherSilenceGrace, 0, true},
		{"started but Docker briefly silent", "STARTED", true, true, up, 0, time.Minute, false},
		{"started but Docker silent past the grace", "STARTED", true, true, up, 0, rancherSilenceGrace, true},
		{"starting with Docker silent", "STARTING", true, true, up, 0, time.Hour, false},
		{"started on macOS with Docker silent", "STARTED", true, false, up, 0, time.Hour, false},
	} {
		if got := vmStuck(tc.state, tc.known, tc.windows, tc.distro, tc.silent, tc.docker); got != tc.want {
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

func TestSilenceTimesOnlyAnUnbrokenStretch(t *testing.T) {
	var s silence
	start := time.Now()
	if got := s.observe(false, start); got != 0 {
		t.Fatalf("first silence = %s, want 0", got)
	}
	if got := s.observe(false, start.Add(time.Minute)); got != time.Minute {
		t.Fatalf("silence = %s, want 1m", got)
	}
	s.observe(true, start.Add(2*time.Minute))
	if got := s.observe(false, start.Add(3*time.Minute)); got != 0 {
		t.Fatalf("an answer did not restart the timer: %s", got)
	}
	s.reset()
	if got := s.observe(false, start.Add(4*time.Minute)); got != 0 {
		t.Fatalf("a launch did not restart the timer: %s", got)
	}
}
