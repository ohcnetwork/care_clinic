package compose

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/lines"
)

func TestLockSurvivesRoundTripAndCorruption(t *testing.T) {
	dir := t.TempDir()
	next := lineFixture("x", 'b').Backend
	want := Lock{
		Line:     "stable",
		Backend:  Channel{Current: lineFixture("x", 'a').Backend, Next: &next, Declined: next.Commit},
		Frontend: Channel{Current: lineFixture("x", 'a').Frontend},
	}
	if err := WriteLock(dir, want); err != nil {
		t.Fatal(err)
	}
	if got := ReadLock(dir); !reflect.DeepEqual(got, want) {
		t.Errorf("lock round-trip = %+v, want %+v", got, want)
	}
	if err := os.WriteFile(filepath.Join(dir, LockFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ReadLock(dir); !reflect.DeepEqual(got, Lock{}) {
		t.Errorf("a corrupt lock was trusted: %+v", got)
	}
	if got := ReadLock(filepath.Join(dir, "absent")); !reflect.DeepEqual(got, Lock{}) {
		t.Errorf("a missing lock was not empty: %+v", got)
	}
}

func installed(t *testing.T) *Builder {
	t.Helper()
	dockerFixture(t)
	t.Setenv("CARE_IMAGE_EXISTS", "yes")
	b := builderFixture(t)
	writeBuildFile(t, b.dir, "src/backend/x", "old checkout")
	writeBuildFile(t, b.dir, legacyLockFile, "{}")
	if err := b.Parallel(EnsureBackend, EnsureFrontend); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFirstInstallRecordsTheDefaultLine(t *testing.T) {
	b := installed(t)
	lock := ReadLock(b.dir)
	want := lineFixture("stable", 'a')
	if lock.Line != "stable" || lock.Backend.Current.Image != want.Backend.Image || lock.Frontend.Current.Image != want.Frontend.Image {
		t.Fatalf("lock = %+v", lock)
	}
	for _, name := range []string{"src", legacyLockFile} {
		if _, err := os.Stat(filepath.Join(b.dir, name)); !os.IsNotExist(err) {
			t.Errorf("source-build leftover %s was kept", name)
		}
	}
}

func TestStartupKeepsTheInstalledReleaseOffline(t *testing.T) {
	b := installed(t)
	if err := os.Remove(b.set.LinesURL); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(b.dir, lines.CacheFile)); err != nil {
		t.Fatal(err)
	}
	if err := b.EnsureBackendImage(); err != nil {
		t.Fatalf("an installed clinic needed the network to start: %v", err)
	}
}

func TestUpdatesAreStagedAppliedAndDeclined(t *testing.T) {
	b := installed(t)
	if u, err := b.PrepareUpdate(); err != nil || u.Any() {
		t.Fatalf("an unchanged line offered an update: %+v %v", u, err)
	}
	l := lineFixture("stable", 'a')
	l.Backend = lineFixture("stable", 'b').Backend
	publish(t, b, lines.Manifest{Lines: []lines.Line{l}})
	u, err := b.PrepareUpdate()
	if err != nil || u.Backend != l.Backend.Commit || u.Frontend != "" {
		t.Fatalf("PrepareUpdate = %+v %v", u, err)
	}
	if w := b.Waiting(); w.Backend != l.Backend.Commit {
		t.Fatalf("Waiting = %+v", w)
	}
	if err := b.DeclineUpdate(); err != nil {
		t.Fatal(err)
	}
	if u, _ := b.PrepareUpdate(); u.Any() {
		t.Fatal("a declined update was offered again")
	}
	applied, err := b.ApplyPending()
	if err != nil || applied.Backend != l.Backend.Commit {
		t.Fatalf("ApplyPending = %+v %v", applied, err)
	}
	lock := ReadLock(b.dir)
	if lock.Backend.Current.Image != l.Backend.Image || lock.Backend.Next != nil || lock.Backend.Declined != "" {
		t.Fatalf("lock after apply = %+v", lock.Backend)
	}
}

func TestLineUpgradeSwitchesTheClinicsLine(t *testing.T) {
	b := installed(t)
	newer := lineFixture("next", 'b')
	needsApp := lineFixture("future", 'c')
	needsApp.MinAppVersion = "9.0.0"
	publish(t, b, lines.Manifest{Lines: []lines.Line{lineFixture("stable", 'a'), newer, needsApp}})
	if u, err := b.PrepareUpdate(); err != nil || u.NewerLine != "next" {
		t.Fatalf("newer line was not offered: %+v %v", u, err)
	}
	if _, err := b.StageLine("stable"); err == nil {
		t.Fatal("staged the current line as an upgrade")
	}
	if _, err := b.StageLine("future"); err == nil {
		t.Fatal("staged a line this app cannot run")
	}
	u, err := b.StageLine("next")
	if err != nil || u.Line != "next" || u.Backend != newer.Backend.Commit || u.Frontend != newer.Frontend.Commit {
		t.Fatalf("StageLine = %+v %v", u, err)
	}
	if u, _ := b.PrepareUpdate(); u.Any() {
		t.Fatal("a routine check replaced a staged line upgrade")
	}
	if _, err := b.ApplyPending(); err != nil {
		t.Fatal(err)
	}
	if lock := ReadLock(b.dir); lock.Line != "next" || lock.NextLine != "" || lock.Frontend.Current.Image != newer.Frontend.Image {
		t.Fatalf("lock after line upgrade = %+v", lock)
	}
}

func TestDecliningALineUpgradeDropsIt(t *testing.T) {
	b := installed(t)
	publish(t, b, lines.Manifest{Lines: []lines.Line{lineFixture("stable", 'a'), lineFixture("next", 'b')}})
	if _, err := b.StageLine("next"); err != nil {
		t.Fatal(err)
	}
	if err := b.DeclineUpdate(); err != nil {
		t.Fatal(err)
	}
	if w := b.Waiting(); w.Any() || w.Line != "" {
		t.Fatalf("a declined line upgrade stayed staged: %+v", w)
	}
	if ReadLock(b.dir).Line != "stable" {
		t.Fatal("declining moved the clinic's line")
	}
}

func TestDroppedStagedBuildIsOfferedAgain(t *testing.T) {
	dockerFixture(t)
	b := builderFixture(t)
	next := lineFixture("x", 'b').Backend
	if err := WriteLock(b.dir, Lock{Backend: Channel{Next: &next, Declined: next.Commit}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ApplyPending(); err != nil {
		t.Fatal(err)
	}
	got := ReadLock(b.dir).Backend
	if got.Next != nil || got.Declined != "" {
		t.Fatalf("a staged build that vanished left %+v; the commit would be blacklisted forever", got)
	}
}
