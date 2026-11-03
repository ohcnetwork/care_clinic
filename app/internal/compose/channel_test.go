package compose

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/release"
)

func TestImageKeyFollowsTheBranchHead(t *testing.T) {
	b := builderFixture(t)
	repo := filepath.Join(t.TempDir(), "remote")
	run := gitFixture(t, repo)
	b.set.BeRepo = repo

	first := commitFixture(t, run, "first")
	before := builtFrom(t, builderAt(t, b))
	second := commitFixture(t, run, "second")
	after := builtFrom(t, builderAt(t, b))

	if first == second {
		t.Fatal("fixture did not move the branch")
	}
	if before == after {
		t.Fatal("the image key ignored a new commit on the tracked branch - the clinic would never update")
	}
	for ref, key := range map[string]string{first: before, second: after} {
		if !contains(key, ref) {
			t.Errorf("image key %q does not name the commit %s it was built from", key, ref)
		}
	}
}

func TestStartupUsesTheRecordedCommitAndBackgroundChecksLookAhead(t *testing.T) {
	b := builderFixture(t)
	repo := filepath.Join(t.TempDir(), "remote")
	run := gitFixture(t, repo)
	b.set.BeRepo = repo
	installed := commitFixture(t, run, "installed")
	if err := WriteLock(b.dir, Lock{Backend: Channel{Ref: "develop", Current: installed}}); err != nil {
		t.Fatal(err)
	}
	upstream := commitFixture(t, run, "upstream")

	if got := builderAt(t, b).BackendRef(); got != installed {
		t.Errorf("startup resolved %s, want the installed commit %s", got, installed)
	}
	if got := builderAt(t, b).Pending().BackendRef(); got != upstream {
		t.Errorf("the background check resolved %s, want the branch head %s", got, upstream)
	}
}

func TestUnreachableRemoteKeepsTheInstalledCommit(t *testing.T) {
	b := builderFixture(t)
	installed := "a749b92794ac175db8839d3d75ca36402a196282"
	if err := WriteLock(b.dir, Lock{Backend: Channel{Ref: "develop", Current: installed}}); err != nil {
		t.Fatal(err)
	}
	b.set.BeRepo = filepath.Join(b.dir, "no-such-repository")
	if got := builderAt(t, b).Pending().BackendRef(); got != installed {
		t.Errorf("offline check resolved %q, want the installed commit", got)
	}
}

func TestPinnedRefIsUsedVerbatim(t *testing.T) {
	b := builderFixture(t)
	pinned := "a749b92794ac175db8839d3d75ca36402a196282"
	b.set.BeRef = pinned
	b.set.BeRepo = filepath.Join(b.dir, "no-such-repository")
	if got := b.BackendRef(); got != pinned {
		t.Errorf("pinned ref resolved to %q, want %q", got, pinned)
	}
	if !release.IsCommitRef(b.Pending().BackendRef()) {
		t.Error("a pinned ref was not honoured by the background check")
	}
}

func TestLockSurvivesRoundTripAndCorruption(t *testing.T) {
	dir := t.TempDir()
	want := Lock{
		Backend:  Channel{Ref: "develop", Current: "aaa", Next: "bbb", Declined: "bbb"},
		Frontend: Channel{Ref: "develop", Current: "ccc"},
	}
	if err := WriteLock(dir, want); err != nil {
		t.Fatal(err)
	}
	if got := ReadLock(dir); got != want {
		t.Errorf("lock round-trip = %+v, want %+v", got, want)
	}
	if err := os.WriteFile(filepath.Join(dir, LockFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ReadLock(dir); got != (Lock{}) {
		t.Errorf("a corrupt lock was trusted: %+v", got)
	}
	if got := ReadLock(filepath.Join(dir, "absent")); got != (Lock{}) {
		t.Errorf("a missing lock was not empty: %+v", got)
	}
}

func builtFrom(t *testing.T, b *Builder) string {
	t.Helper()
	key, err := b.backendBuiltFrom("")
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func builderAt(t *testing.T, b *Builder) *Builder {
	t.Helper()
	return NewBuilder(b.run, b.dir, b.set, nil)
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		(haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestUnresolvableRefNeverBecomesAnImageKey(t *testing.T) {
	b := builderFixture(t)
	b.set.BeRef = "no-such-branch"
	if _, err := builderAt(t, b).backendBuiltFrom(""); err == nil {
		t.Fatal("an unresolvable branch produced an image key; it would be constant and the clinic would never update")
	}
}

func TestPoisonedLockIsIgnoredRatherThanTrusted(t *testing.T) {
	b := builderFixture(t)
	repo := filepath.Join(t.TempDir(), "remote")
	run := gitFixture(t, repo)
	b.set.BeRepo = repo
	head := commitFixture(t, run, "first")

	if err := WriteLock(b.dir, Lock{Backend: Channel{Ref: "develop", Current: "develop"}}); err != nil {
		t.Fatal(err)
	}
	if got := builderAt(t, b).BackendRef(); got != head {
		t.Fatalf("branch name in the lock was trusted: got %q, want %q", got, head)
	}
}

func TestMissingStagedImageFailsPromotionAndKeepsTheOffer(t *testing.T) {
	dockerFixture(t)
	b := builderFixture(t)
	sha := "1111111111111111111111111111111111111111"
	want := Lock{Backend: Channel{Ref: "develop", Current: "0000000000000000000000000000000000000000", Next: sha}}
	if err := WriteLock(b.dir, want); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ApplyPending(); err == nil {
		t.Fatal("promotion reported success although the staged image is missing")
	}
	if got := ReadLock(b.dir); got != want {
		t.Fatalf("a failed promotion changed the lock: %+v", got)
	}
}

func TestDiscoverUpdateOnlyReadsMetadata(t *testing.T) {
	trace := dockerFixture(t)
	b := builderFixture(t)
	repo := filepath.Join(t.TempDir(), "remote")
	run := gitFixture(t, repo)
	b.set.BeRepo = repo
	installed := commitFixture(t, run, "installed")
	if err := WriteLock(b.dir, Lock{Backend: Channel{Ref: "develop", Current: installed}}); err != nil {
		t.Fatal(err)
	}
	upstream := commitFixture(t, run, "upstream")
	b.set.FeRef = "a749b92794ac175db8839d3d75ca36402a196282"

	got, err := b.DiscoverUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if got.Backend != upstream || got.Frontend != "" {
		t.Fatalf("discovered %+v, want only the backend head %s", got, upstream)
	}
	if lock := ReadLock(b.dir).Backend; lock.Next != upstream || lock.Current != installed {
		t.Fatalf("lock after discovery: %+v", lock)
	}
	if data, _ := os.ReadFile(trace); len(data) != 0 {
		t.Fatalf("discovery ran Docker commands: %q", data)
	}
	if _, err := os.Stat(filepath.Join(b.dir, "src")); !os.IsNotExist(err) {
		t.Fatalf("discovery downloaded source code: %v", err)
	}
}

func TestDiscoverUpdateRespectsDeclinesAndOfflineChecks(t *testing.T) {
	b := builderFixture(t)
	repo := filepath.Join(t.TempDir(), "remote")
	run := gitFixture(t, repo)
	b.set.BeRepo = repo
	installed := commitFixture(t, run, "installed")
	upstream := commitFixture(t, run, "upstream")
	declined := Lock{Backend: Channel{Ref: "develop", Current: installed, Next: upstream, Declined: upstream}}
	b.set.FeRef = "a749b92794ac175db8839d3d75ca36402a196282"
	if err := WriteLock(b.dir, declined); err != nil {
		t.Fatal(err)
	}
	if got, err := b.DiscoverUpdate(); err != nil || got.Any() {
		t.Fatalf("a declined commit was offered again: %+v, %v", got, err)
	}
	if got := ReadLock(b.dir); got != declined {
		t.Fatalf("a declined offer was rewritten: %+v", got)
	}
	b.set.BeRepo = filepath.Join(b.dir, "no-such-repository")
	if got, err := b.DiscoverUpdate(); err != nil || got.Any() {
		t.Fatalf("an unreachable remote produced %+v, %v", got, err)
	}
	if got := ReadLock(b.dir); got != declined {
		t.Fatalf("an offline check changed the lock: %+v", got)
	}
}

func TestDiscoverUpdateForgetsAnOfferTheBranchNoLongerHas(t *testing.T) {
	b := builderFixture(t)
	repo := filepath.Join(t.TempDir(), "remote")
	run := gitFixture(t, repo)
	b.set.BeRepo = repo
	installed := commitFixture(t, run, "installed")
	stale := "2222222222222222222222222222222222222222"
	b.set.FeRef = "a749b92794ac175db8839d3d75ca36402a196282"
	if err := WriteLock(b.dir, Lock{Backend: Channel{Ref: "develop", Current: installed, Next: stale, Declined: stale}}); err != nil {
		t.Fatal(err)
	}
	if got, err := b.DiscoverUpdate(); err != nil || got.Any() {
		t.Fatalf("an up-to-date clinic was offered %+v, %v", got, err)
	}
	if got := ReadLock(b.dir).Backend; got.Next != "" || got.Declined != "" {
		t.Fatalf("a vanished candidate stayed on offer: %+v", got)
	}
}

func TestPendingAtBuildsTheOfferedCommitsEvenAfterTheBranchMoves(t *testing.T) {
	b := builderFixture(t)
	repo := filepath.Join(t.TempDir(), "remote")
	run := gitFixture(t, repo)
	b.set.BeRepo = repo
	installed := commitFixture(t, run, "installed")
	offered := commitFixture(t, run, "offered")
	if err := WriteLock(b.dir, Lock{Backend: Channel{Ref: "develop", Current: installed, Next: offered}}); err != nil {
		t.Fatal(err)
	}
	moved := commitFixture(t, run, "moved")

	next := builderAt(t, b).PendingAt(Update{Backend: offered})
	if got := next.BackendRef(); got != offered {
		t.Fatalf("install resolved %s, want the offered commit %s (branch head is %s)", got, offered, moved)
	}
	if got := next.worker(context.Background(), Backend).BackendRef(); got != offered {
		t.Fatalf("an install worker resolved %s, want the offered commit %s", got, offered)
	}
	if got := builderAt(t, b).Pending().BackendRef(); got != moved {
		t.Fatalf("an unpinned pending builder resolved %s, want the branch head %s", got, moved)
	}
}
