package compose

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
)

type Job struct {
	Label string
	Run   func(*Builder) error
}

var (
	EnsureBackend  = Job{Label: Backend, Run: (*Builder).EnsureBackendImage}
	EnsureFrontend = Job{Label: Frontend, Run: (*Builder).EnsureFrontendImage}
	EnsureBackup   = Job{Label: "backup", Run: (*Builder).EnsureBackupImage}
	EnsureCaddy    = Job{Label: "caddy", Run: (*Builder).EnsureCaddyImage}
	BuildBackend   = Job{Label: Backend, Run: (*Builder).BuildBackend}
	BuildFrontend  = Job{Label: Frontend, Run: (*Builder).BuildFrontend}
)

type Builds struct {
	owner  *Builder
	cancel context.CancelFunc
	done   chan struct{}
	failed sync.Once
	err    error
	waited sync.Once
}

func (b *Builder) Parallel(jobs ...Job) error {
	return b.Start(jobs...).Wait()
}

func (b *Builder) Start(jobs ...Job) *Builds {
	ctx, cancel := context.WithCancel(context.Background())
	g := &Builds{owner: b, cancel: cancel, done: make(chan struct{})}
	b.groups.Add(1)
	var wg sync.WaitGroup
	for _, job := range jobs {
		w := b.worker(ctx, job.Label)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := runJob(w, job); err != nil {
				g.fail(fmt.Errorf("%s image: %w", job.Label, err))
			}
		}()
	}
	go func() {
		wg.Wait()
		close(g.done)
	}()
	return g
}

func runJob(w *Builder, job Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			w.logln(fmt.Sprintf("PANIC: %v\n%s", r, debug.Stack()))
			err = fmt.Errorf("internal error: %v", r)
		}
	}()
	return job.Run(w)
}

func (g *Builds) fail(err error) {
	g.failed.Do(func() {
		g.err = err
		g.cancel()
	})
}

func (g *Builds) Wait() error {
	g.waited.Do(func() {
		<-g.done
		g.cancel()
		if g.owner.groups.Add(-1) == 0 && g.owner.rebuilt.Swap(false) {
			g.owner.pruneDangling()
		}
	})
	return g.err
}

func (g *Builds) Cancel() {
	g.fail(context.Canceled)
	_ = g.Wait()
}

func (b *Builder) worker(ctx context.Context, label string) *Builder {
	w := NewBuilder(b.run, b.dir, b.set, prefixed(label, b.Log))
	w.run.Ctx = ctx
	w.run.Log = prefixed(label, b.run.Log)
	w.Stop = b.Stop
	w.pending = b.pending
	w.owner = b
	return w
}

func prefixed(label string, log func(string)) func(string) {
	if log == nil {
		return nil
	}
	return func(s string) { log("[" + label + "] " + s) }
}
