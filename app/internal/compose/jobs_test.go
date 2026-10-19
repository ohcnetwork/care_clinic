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

func jobsFixture(t *testing.T) (*Builder, *lines) {
	t.Helper()
	log := &lines{}
	dir := t.TempDir()
	return NewBuilder(proc.Runner{Dir: dir, Log: log.add}, dir, &release.Pins{}, log.add), log
}

func TestSequenceRunsJobsOneAtATime(t *testing.T) {
	b, _ := jobsFixture(t)
	var mu sync.Mutex
	running, order := 0, []string{}
	job := func(label string) Job {
		return Job{Label: label, Run: func(*Builder) error {
			mu.Lock()
			running++
			overlap := running > 1
			order = append(order, label)
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			mu.Lock()
			running--
			mu.Unlock()
			if overlap {
				return errors.New(label + " ran alongside another job")
			}
			return nil
		}}
	}
	if err := b.Sequence(job("backend"), job("frontend"), job("caddy")); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(order, []string{"backend", "frontend", "caddy"}) {
		t.Fatalf("jobs ran out of order: %q", order)
	}
}

func TestSequenceFailureSkipsTheRemainingJobs(t *testing.T) {
	b, _ := jobsFixture(t)
	boom := errors.New("boom")
	ran := false
	err := b.Sequence(
		Job{Label: "fails", Run: func(*Builder) error { return boom }},
		Job{Label: "later", Run: func(*Builder) error { ran = true; return nil }},
	)
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "fails image") {
		t.Fatalf("want the failure named after its job, got %v", err)
	}
	if ran {
		t.Fatal("a job ran after an earlier one failed")
	}
}

func TestSequencePrefixesEachJobsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses echo")
	}
	b, log := jobsFixture(t)
	err := b.Sequence(Job{Label: "backend", Run: func(w *Builder) error {
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

func TestSequencePreservesTheFailedWorkersNetworkCause(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	b, _ := jobsFixture(t)
	err := b.Sequence(
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

func TestSequenceDoesNotUseASuccessfulWorkersNetworkWarning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	b, _ := jobsFixture(t)
	warned := make(chan struct{})
	compile := errors.New("compile failed")
	err := b.Sequence(
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

func TestSequenceRecoversAPanickingJob(t *testing.T) {
	b, _ := jobsFixture(t)
	err := b.Sequence(Job{Label: "caddy", Run: func(*Builder) error { panic("bad") }})
	if err == nil || !strings.Contains(err.Error(), "internal error: bad") {
		t.Fatalf("want the panic reported as an error, got %v", err)
	}
}

func TestCancelStopsRunningJobsAndWaitsForThem(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sleep")
	}
	b, _ := jobsFixture(t)
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
	b, _ := jobsFixture(t)
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
