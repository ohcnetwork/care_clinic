package clinic

import (
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/compose"
	"github.com/ohcnetwork/care_desktop/app/internal/release"
)

func TestStagedUpdateIsHeldWhileUpdateChecksAreOff(t *testing.T) {
	dir := t.TempDir()
	staged := compose.Lock{
		Backend:  compose.Channel{Ref: "develop", Current: "backend-current", Next: "backend-next"},
		Frontend: compose.Channel{Ref: "develop", Current: "frontend-current", Next: "frontend-next"},
	}
	if err := compose.WriteLock(dir, staged); err != nil {
		t.Fatal(err)
	}
	var logged []string
	e := &Clinic{InstallDir: dir, Pins: &release.Pins{}, HoldUpdates: true, Log: func(s string) { logged = append(logged, s) }}
	if err := e.applyStagedUpdate(); err != nil {
		t.Fatalf("held update failed the start: %v", err)
	}
	if got := compose.ReadLock(dir); got != staged {
		t.Fatalf("held update changed the channel lock: %+v", got)
	}
	if len(logged) != 1 {
		t.Fatalf("held update was not explained in the log: %q", logged)
	}
}
