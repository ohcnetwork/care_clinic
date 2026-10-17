package main

import (
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/mdns"
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

type fakeAdvertiser struct {
	checks   []error
	changes  []bool
	resolves error
	checked  int
	stopped  bool
}

func (f *fakeAdvertiser) Name() string { return "care" }

func (f *fakeAdvertiser) IPsChanged() (bool, error) {
	if len(f.changes) == 0 {
		return false, nil
	}
	changed := f.changes[0]
	f.changes = f.changes[1:]
	return changed, nil
}

func (f *fakeAdvertiser) CheckAvailable() error {
	f.checked++
	if len(f.checks) == 0 {
		return nil
	}
	err := f.checks[0]
	f.checks = f.checks[1:]
	return err
}

func (f *fakeAdvertiser) Resolves() error { return f.resolves }

func (f *fakeAdvertiser) Stop() { f.stopped = true }

func duplicate() error {
	return &mdns.ConflictError{Host: "care.local", Address: net.ParseIP("172.20.10.4")}
}

func advertisingApp(t *testing.T, adv *fakeAdvertiser, since time.Time) *App {
	t.Helper()
	a := roleApp(t)
	a.cfg = Config{Role: roleServer}
	a.adv = adv
	a.advSince = since
	return a
}

func reported(a *App) bool {
	var conflict *mdns.ConflictError
	return errors.As(a.advError, &conflict)
}

func TestWatcherRechecksBeforeReportingConflict(t *testing.T) {
	adv := &fakeAdvertiser{checks: []error{duplicate(), nil}}
	a := advertisingApp(t, adv, time.Time{})
	var w advertiseWatch
	now := time.Now()
	if next := a.checkAdvertise(&w, now); next != advertiseRecheck {
		t.Fatalf("first sighting waited %v, want a quick recheck", next)
	}
	if reported(a) || adv.stopped || !a.advRunning() {
		t.Fatal("a single sighting withdrew the name or notified the operator")
	}
	if next := a.checkAdvertise(&w, now.Add(advertiseRecheck)); next != advertiseInterval {
		t.Fatalf("clean recheck scheduled %v", next)
	}
	if reported(a) || adv.stopped || w.sightings != 0 {
		t.Fatal("a sighting followed by a clean check was reported")
	}
}

func TestWatcherReportsConsistentConflict(t *testing.T) {
	adv := &fakeAdvertiser{checks: []error{duplicate(), duplicate()}}
	a := advertisingApp(t, adv, time.Time{})
	var w advertiseWatch
	now := time.Now()
	a.checkAdvertise(&w, now)
	a.checkAdvertise(&w, now.Add(advertiseRecheck))
	if !reported(a) || !adv.stopped || a.advRunning() {
		t.Fatal("two consecutive sightings did not withdraw the name and notify")
	}
}

func TestWatcherNetworkChangeResetsSightings(t *testing.T) {
	for name, adv := range map[string]*fakeAdvertiser{
		"between checks": {checks: []error{duplicate(), duplicate()}, changes: []bool{false, false, true}},
		"during check":   {checks: []error{duplicate()}, changes: []bool{false, true}},
		"bind failure": {checks: []error{duplicate(), &mdns.ConflictError{
			Host: "care.local", Address: net.ParseIP("172.20.10.4"),
			Unsettled: &net.OpError{Op: "listen", Net: "udp4", Err: syscall.EADDRNOTAVAIL},
		}}},
	} {
		t.Run(name, func(t *testing.T) {
			a := advertisingApp(t, adv, time.Time{})
			var w advertiseWatch
			now := time.Now()
			a.checkAdvertise(&w, now)
			a.checkAdvertise(&w, now.Add(advertiseRecheck))
			if reported(a) || w.sightings != 0 {
				t.Fatalf("a conflict across a network change was reported (sightings %d)", w.sightings)
			}
			if !adv.stopped || a.advRunning() {
				t.Fatal("the advertiser was not restarted after the network change")
			}
		})
	}
}

func TestWatcherSkipsConflictCheckWhileSettling(t *testing.T) {
	now := time.Now()
	adv := &fakeAdvertiser{checks: []error{duplicate(), duplicate()}}
	a := advertisingApp(t, adv, now.Add(-advertiseSettle/2))
	w := advertiseWatch{sightings: 1}
	if next := a.checkAdvertise(&w, now); next != advertiseInterval {
		t.Fatalf("settling check scheduled %v", next)
	}
	if adv.checked != 0 || reported(a) || w.sightings != 0 {
		t.Fatal("checked for conflicts straight after the advertiser started")
	}
	a.checkAdvertise(&w, now.Add(advertiseSettle))
	if adv.checked != 1 || reported(a) {
		t.Fatal("did not resume conflict checks once the network settled")
	}
}

func TestMDNSStatusLeavesAdvertisedNameToWatcher(t *testing.T) {
	adv := &fakeAdvertiser{}
	a := advertisingApp(t, adv, time.Time{})
	a.cfg.SetupDone = true
	probes := 0
	a.nameCheck = func(string) error {
		probes++
		return duplicate()
	}
	if status := a.MDNSStatus("care.local"); !status.OK || probes != 0 {
		t.Fatalf("one probe flagged the name this computer advertises: %+v", status)
	}
	if status := a.MDNSStatus("other"); status.OK || !strings.Contains(status.Message, "already in use") || probes != 1 {
		t.Fatalf("a name this computer does not advertise skipped the strict check: %+v", status)
	}
	a.adv = nil
	if status := a.MDNSStatus("care"); status.OK || !strings.Contains(status.Message, "already in use") {
		t.Fatalf("a withdrawn name did not report the conflict: %+v", status)
	}
}

func TestMDNSStatusBeforeSetupStaysStrict(t *testing.T) {
	a := roleApp(t)
	a.cfg = Config{Role: roleServer}
	a.nameCheck = func(string) error { return duplicate() }
	if status := a.MDNSStatus("care"); status.OK || !strings.Contains(status.Message, "already in use") {
		t.Fatalf("setup accepted a name another device answers for: %+v", status)
	}
	a.nameCheck = func(string) error { return nil }
	if status := a.MDNSStatus("care"); !status.OK {
		t.Fatalf("setup rejected a free name: %+v", status)
	}
}
