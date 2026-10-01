package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/trust"
)

// The client page maps backend errors to plain language by prefix, so looking
// for a clinic and connecting to it must fail with the same words.
func TestFindClinicRejectsTheSameAddressesAsConnect(t *testing.T) {
	a := roleApp(t)
	for _, address := range []string{"", "care.local:443", "care.local/setup", "127.0.0.1", "clinic.example.com"} {
		_, found := a.FindClinic(address)
		connect := a.ConnectClient(address)
		if found == nil || connect == nil || found.Error() != connect.Error() {
			t.Fatalf("%q: find = %v, connect = %v", address, found, connect)
		}
		if _, err := trust.ClientURL(address); err == nil || err.Error() != found.Error() {
			t.Fatalf("%q: the address error was rewritten: %v", address, found)
		}
	}
}

func TestFindClinicChangesNothingOnThisComputer(t *testing.T) {
	a := roleApp(t)
	// care-desktop-not-a-clinic.local cannot be in any hosts file, so this
	// reaches the network and fails without touching the system.
	if _, err := a.FindClinic("care-desktop-not-a-clinic"); err == nil {
		t.Fatal("a clinic answered at a name that cannot exist")
	}
	if _, err := os.Stat(a.configPath()); !os.IsNotExist(err) {
		t.Fatalf("looking for a clinic wrote settings: %v", err)
	}
	if a.loadConfig() != (Config{}) {
		t.Fatalf("looking for a clinic changed settings: %+v", a.loadConfig())
	}
	if a.clinicRoot("https://care-desktop-not-a-clinic.local") != "" {
		t.Fatal("a failed search remembered a certificate")
	}
}

// The root FindClinic validated is reused by Connect, so a clinic is downloaded
// on first use once rather than twice.
func TestRememberedRootIsReusedForTheSameClinicOnly(t *testing.T) {
	a := roleApp(t)
	a.rememberClinicRoot("https://care.local", "public root")
	if got := a.clinicRoot("https://care.local"); got != "public root" {
		t.Fatalf("clinicRoot = %q", got)
	}
	if got := a.clinicRoot("https://other.local"); got != "" {
		t.Fatalf("another clinic reused the certificate: %q", got)
	}
}

// A name no computer can claim proves the bypass is only attempted when the
// hosts file actually redirects the clinic.
func TestClinicDialAddressIsEmptyWithoutAHostsEntry(t *testing.T) {
	at, err := clinicDialAddress("care-desktop-not-a-clinic.local")
	if err != nil || at != "" {
		t.Fatalf("clinicDialAddress = %q, %v", at, err)
	}
}

func TestClientReachableNeedsASavedClinicAndNeverErrorsOnSilence(t *testing.T) {
	a := roleApp(t)
	if _, err := a.ClientReachable(); err == nil {
		t.Fatal("a computer with no clinic reported on one")
	}
	// A name no computer can claim: nothing answers, which is a state, not a fault.
	cfg := Config{Role: roleClient, ClientURL: "https://care-desktop-not-a-clinic.local"}
	if err := a.saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	before := time.Now().Unix()
	state, err := a.ClientReachable()
	if err != nil {
		t.Fatalf("an unreachable clinic must not be an error: %v", err)
	}
	if state.Reachable || state.Detail == "" || state.CheckedAt < before {
		t.Fatalf("ClientReachable = %+v", state)
	}
	if a.loadConfig() != cfg {
		t.Fatalf("the probe changed settings: %+v", a.loadConfig())
	}
	if _, err := os.Stat(a.installDir()); !os.IsNotExist(err) {
		t.Fatalf("the probe installed something: %v", err)
	}
}

// The screen polls this on a timer, so a connection in progress must not turn
// every poll into "something else is still running".
func TestClientReachableDoesNotWaitForARunningJob(t *testing.T) {
	a := roleApp(t)
	if err := a.saveConfig(Config{Role: roleClient, ClientURL: "https://care-desktop-not-a-clinic.local"}); err != nil {
		t.Fatal(err)
	}
	a.jobMu.Lock()
	defer a.jobMu.Unlock()
	if _, err := a.ClientReachable(); err != nil {
		t.Fatalf("the poll was refused while another job held the lock: %v", err)
	}
}

func TestClientPreflightReadsWithoutChangingAnything(t *testing.T) {
	a := roleApp(t)
	result, err := a.ClientPreflight()
	if err != nil {
		t.Skipf("this computer's certificate stores could not be inspected: %v", err)
	}
	if result.UnfinishedServerSetup {
		t.Fatalf("a fresh computer reported an unfinished clinic setup: %+v", result)
	}
	if _, err := os.Stat(a.configPath()); !os.IsNotExist(err) {
		t.Fatalf("the preflight wrote settings: %v", err)
	}

	if err := os.MkdirAll(a.installDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.installDir()+"/earlier-installation", []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = a.ClientPreflight()
	if err != nil || !result.UnfinishedServerSetup {
		t.Fatalf("a half-installed clinic was not reported: %+v, %v", result, err)
	}
}

func TestClientErrorsStayPlainAndPrefixMatchable(t *testing.T) {
	for _, message := range []string{selfAddressedClinic, unfinishedServerSetup} {
		if message == "" || strings.ToUpper(message[:1]) == message[:1] || strings.HasSuffix(message, ".") {
			t.Fatalf("client errors are lower-case sentences without a full stop: %q", message)
		}
		for _, jargon := range []string{"Docker", "WSL", "mDNS", "TLS", "hosts file", "DNS", "keychain", "certificate store"} {
			if strings.Contains(message, jargon) {
				t.Fatalf("%q leaks %q into the interface", message, jargon)
			}
		}
	}
}
