package main

import (
	"os"
	"strings"
	"testing"
)

func TestSetupDoesNotAdvertiseChosenName(t *testing.T) {
	a := roleApp(t)
	a.cfg = Config{Role: roleServer}
	if err := a.SetMDNSName("care"); err != nil {
		t.Fatal(err)
	}
	if a.advRunning() || a.loadConfig().MDNSName != "care.local" {
		t.Fatal("setup must save the chosen name without claiming it on the LAN")
	}
	if err := a.startAdvertise(); err != nil || a.advRunning() {
		t.Fatalf("watcher advertised an uninstalled clinic: %v", err)
	}
}

func TestMDNSStatusRejectsInvalidName(t *testing.T) {
	a := roleApp(t)
	for _, name := range []string{"", "bad name", "care.example.com"} {
		if status := a.MDNSStatus(name); status.OK || status.Message == "" {
			t.Fatalf("invalid hostname reported ready: %+v", status)
		}

	}
}

func TestOccupiedNameBlocksSetupStatusOverLAN(t *testing.T) {
	host := os.Getenv("CARE_MDNS_CONFLICT_TEST_HOST")
	if host == "" {
		t.Skip("set CARE_MDNS_CONFLICT_TEST_HOST to an existing remote clinic hostname")
	}
	a := roleApp(t)
	a.cfg = Config{Role: roleServer, MDNSName: "different-saved-name.local"}
	status := a.MDNSStatus(host)
	if status.OK || !strings.Contains(status.Message, "already in use") {
		t.Fatalf("setup did not reject the name currently entered in the form: %+v", status)
	}
	if a.advRunning() {
		t.Fatal("checking an occupied name must not advertise it")
	}
	t.Log(status.Message)
}
