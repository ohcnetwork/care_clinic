package clinic

import (
	"errors"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
)

func TestLocalSetupRequiresVerifiedHostsAndTrust(t *testing.T) {
	for _, tc := range []struct {
		name       string
		hostsReady bool
		trustReady bool
		err        error
		want       string
	}{
		{"verified", true, true, nil, "can now open"},
		{"verified despite command failure", true, true, errors.New("command failed"), "can now open"},
		{"missing hosts", false, true, nil, "Could not confirm the local hosts entry."},
		{"missing trust", true, false, nil, "Could not confirm certificate trust."},
		{"missing both", false, false, nil, "Could not confirm the local hosts entry and certificate trust."},
		{"failed elevation", true, false, errors.New("approval declined"), "certificate trust (approval declined)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := localSetupResult("care.local", tc.hostsReady, tc.trustReady, tc.err)
			if !strings.Contains(message, tc.want) {
				t.Fatalf("unexpected setup result: %q", message)
			}
			if (!tc.hostsReady || !tc.trustReady) &&
				(strings.Contains(message, "can now open") || !strings.Contains(message, "Other devices are unaffected")) {
				t.Fatalf("optional local setup failure was misreported: %q", message)
			}
			if strings.Contains(message, "/setup") {
				t.Fatalf("setup result refers to the retired web page: %q", message)
			}
			if (!tc.hostsReady || !tc.trustReady) && !strings.Contains(message, "try starting CARE again") {
				t.Fatalf("setup failure must explain how to retry: %q", message)
			}
		})
	}
}

func TestConfirmPromptSaysHowEachSystemAsks(t *testing.T) {
	one := []elevate.Step{{What: "trust CARE's security certificate"}}
	two := append(one, elevate.Step{What: "add care.local to this computer's hosts file"})
	for _, tc := range []struct {
		goos  string
		steps []elevate.Step
		want  string
	}{
		{"darwin", one, "computer's password or Touch ID"},
		{"darwin", two, "computer's administrator password and approve the security prompt"},
		{"windows", two, "Approve the Windows permission prompt"},
		{"linux", two, "administrator password"},
	} {
		title, message := confirmPrompt(tc.goos, "care.local", tc.steps)
		if title != "Set up care.local on this computer?" {
			t.Fatalf("unexpected title %q", title)
		}
		if !strings.Contains(message, tc.want) || strings.Contains(message, "password once") {
			t.Fatalf("%s with %d steps: unexpected message %q", tc.goos, len(tc.steps), message)
		}
		if len(strings.Fields(message)) > 35 {
			t.Fatalf("permission prompt is too wordy: %q", message)
		}
		for _, technical := range []string{"hosts file", "keychain", "•", "needs to:"} {
			if strings.Contains(message, technical) {
				t.Fatalf("permission prompt exposes technical setup details: %q", message)
			}
		}
	}
}

func TestMacPutsTheCertificateInTheAdminBatchWhenOneIsNeededAnyway(t *testing.T) {
	for _, tc := range []struct {
		goos      string
		elevating bool
		want      bool
	}{
		{"darwin", true, false},
		{"darwin", false, true},
		{"windows", true, true},
		{"linux", true, true},
	} {
		if got := trustWithoutAdminFirst(tc.goos, tc.elevating); got != tc.want {
			t.Fatalf("%s, elevating=%v: got %v, want %v", tc.goos, tc.elevating, got, tc.want)
		}
	}
}
