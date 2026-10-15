package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/clinic"
)

func TestFailedEnableKeepsTheRestartForTheRetry(t *testing.T) {
	dir := t.TempDir()
	frontend := filepath.Join(dir, "frontend.env")
	if err := os.WriteFile(frontend, []byte("REACT_CARE_API_URL=https://north.local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &clinic.Clinic{InstallDir: dir, MDNSName: "north"}

	if _, err := enableHomeAccess(e, func() error { return errors.New("tailscale serve failed") }); err == nil {
		t.Fatal("expected the serve failure")
	}
	if got, _ := os.ReadFile(frontend); !strings.Contains(string(got), "https://north.local") {
		t.Fatalf("settings changed although serving failed: %q", got)
	}

	restart, err := enableHomeAccess(e, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if !restart {
		t.Fatal("the retry must still restart the clinic to apply the new settings")
	}
	if got, _ := os.ReadFile(frontend); string(got) != "REACT_CARE_API_URL=\n" {
		t.Fatalf("settings were not updated: %q", got)
	}
}
