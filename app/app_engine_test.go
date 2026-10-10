package main

import (
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/vm"
)

func TestEngineStateDetectsOutdatedEngine(t *testing.T) {
	cases := []struct {
		name      string
		installed string
		toolsOK   bool
		status    vm.HostStatus
		want      bool
	}{
		{"current and stopped", "0.1.1", true, vm.HostStatus{}, false},
		{"current and running", "0.1.1", true, vm.HostStatus{PID: 7, Version: "0.1.1", Logs: vm.LogsCollecting}, false},
		{"old engine installed", "0.1.0", true, vm.HostStatus{}, true},
		{"old engine still running after app update", "0.1.1", true, vm.HostStatus{PID: 7, Version: "0.1.0"}, true},
		{"tools missing", "0.1.1", false, vm.HostStatus{}, true},
		{"not installed", "", true, vm.HostStatus{}, false},
	}
	for _, tc := range cases {
		st := engineState(tc.installed, "0.1.1", tc.toolsOK, tc.status)
		if st.UpdateNeeded != tc.want {
			t.Errorf("%s: update needed = %v, want %v", tc.name, st.UpdateNeeded, tc.want)
		}
		if st.Running != (tc.status.PID != 0) || st.Logs != tc.status.Logs {
			t.Errorf("%s: unexpected state %+v", tc.name, st)
		}
	}
}

func TestEngineUpdateIsAnAllowedClinicAction(t *testing.T) {
	if got := failureTitle("engine-update"); got != "The CARE engine update didn't finish" {
		t.Fatalf("failure title = %q", got)
	}
}
