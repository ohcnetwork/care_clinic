package compose

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/release"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

type lines struct {
	mu  sync.Mutex
	all []string
}

func (l *lines) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.all = append(l.all, s)
}

func (l *lines) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.all)
}

func parallelFixture(t *testing.T) (*Builder, *lines) {
	t.Helper()
	log := &lines{}
	dir := t.TempDir()
	return NewBuilder(proc.Runner{Dir: dir, Log: log.add}, dir, &release.Pins{}, log.add), log
}

func TestParallelRunsJobsAtTheSameTime(t *testing.T) {
	b, _ := parallelFixture(t)
	aStarted, bStarted := make(chan struct{}), make(chan struct{})
	meet := func(mine, theirs chan struct{}) func(*Builder) error {
		return func(*Builder) error {
			close(mine)
			select {
			case <-theirs:
				return nil
			case <-time.After(5 * time.Second):
				return errors.New("the other job never started")
			}
		}
	}
	err := b.Parallel(Job{Label: "a", Run: meet(aStarted, bStarted)}, Job{Label: "b", Run: meet(bStarted, aStarted)})
	if err != nil {
		t.Fatal(err)
	}
}

func TestParallelFailureStopsTheOtherJobs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sleep")
	}
	b, _ := parallelFixture(t)
	boom := errors.New("boom")
	started := time.Now()
	err := b.Parallel(
		Job{Label: "fails", Run: func(*Builder) error { return boom }},
		Job{Label: "slow", Run: func(w *Builder) error { return w.run.Run("sleep", "30") }},
	)
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "fails image") {
		t.Fatalf("want the first failure named after its job, got %v", err)
	}
	if time.Since(started) > 10*time.Second {
		t.Fatal("the slow job was not stopped after the other one failed")
	}
}

func TestParallelPrefixesEachJobsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses echo")
	}
	b, log := parallelFixture(t)
	err := b.Parallel(Job{Label: "backend", Run: func(w *Builder) error {
		w.logln("Building the backend image")
		return w.run.Run("echo", "step 1/9")
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := log.snapshot()
	for _, want := range []string{"[backend] Building the backend image", "[backend] step 1/9"} {
		if !slices.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestParallelPreservesTheFailedWorkersNetworkCause(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	b, _ := parallelFixture(t)
	err := b.Parallel(
		Job{Label: "caddy", Run: func(w *Builder) error {
			return w.run.Run("/bin/sh", "-c", `printf 'Get "https://example.invalid/module": unexpected EOF\n' >&2; exit 1`)
		}},
		Job{Label: "backend", Run: func(w *Builder) error { return w.run.Run("sleep", "30") }},
	)
	var network *proc.NetworkError
	if !errors.As(err, &network) || !strings.Contains(err.Error(), "caddy image") {
		t.Fatalf("the failed download was replaced by its cancelled sibling: %v", err)
	}
}

func TestParallelDoesNotUseASuccessfulWorkersNetworkWarning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	b, _ := parallelFixture(t)
	warned := make(chan struct{})
	compile := errors.New("compile failed")
	err := b.Parallel(
		Job{Label: "backend", Run: func(w *Builder) error {
			defer close(warned)
			return w.run.Run("/bin/sh", "-c", "printf 'connection reset by peer; retry succeeded\\n'")
		}},
		Job{Label: "frontend", Run: func(*Builder) error { <-warned; return compile }},
	)
	var network *proc.NetworkError
	if !errors.Is(err, compile) || errors.As(err, &network) {
		t.Fatalf("another image's warning changed the reported failure: %v", err)
	}
}

func TestParallelRecoversAPanickingJob(t *testing.T) {
	b, _ := parallelFixture(t)
	err := b.Parallel(Job{Label: "caddy", Run: func(*Builder) error { panic("bad") }})
	if err == nil || !strings.Contains(err.Error(), "internal error: bad") {
		t.Fatalf("want the panic reported as an error, got %v", err)
	}
}

func TestCancelStopsRunningJobsAndWaitsForThem(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sleep")
	}
	b, _ := parallelFixture(t)
	running := make(chan struct{})
	finished := false
	g := b.Start(Job{Label: "slow", Run: func(w *Builder) error {
		close(running)
		err := w.run.Run("sleep", "30")
		finished = true
		return err
	}})
	<-running
	started := time.Now()
	g.Cancel()
	if !finished || time.Since(started) > 10*time.Second {
		t.Fatal("Cancel returned before the job stopped, or the job was not stopped")
	}
	if err := g.Wait(); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled after Cancel, got %v", err)
	}
}

func TestRebuildInsideAGroupDefersCleanupToTheLastGroup(t *testing.T) {
	b, _ := parallelFixture(t)
	first := b.Start(Job{Label: "a", Run: func(w *Builder) error { w.afterRebuild(); return nil }})
	second := b.Start(Job{Label: "b", Run: func(*Builder) error { return nil }})
	if err := first.Wait(); err != nil {
		t.Fatal(err)
	}
	if !b.rebuilt.Load() {
		t.Fatal("cleanup ran while another group was still open")
	}
	if err := second.Wait(); err != nil {
		t.Fatal(err)
	}
	if b.rebuilt.Load() {
		t.Fatal("cleanup was not run once the last group finished")
	}
}
