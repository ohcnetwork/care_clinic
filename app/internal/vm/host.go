package vm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/applog"
)

// HostStatus is what the helper reports on its control socket.
type HostStatus struct {
	Running   bool   `json:"running"`
	Docker    bool   `json:"docker"`
	Version   string `json:"version"`
	Error     string `json:"error,omitempty"`
	DiskFree  uint64 `json:"disk_free"`
	DiskTotal uint64 `json:"disk_total"`
	PID       int    `json:"pid"`
	WebPorts  []int  `json:"web_ports,omitempty"`
	// Logs is empty when the helper predates guest log collection.
	Logs string `json:"logs,omitempty"`
}

// Guest log collection states reported in HostStatus.Logs.
const (
	LogsConnecting  = "connecting"
	LogsCollecting  = "collecting"
	LogsInterrupted = "interrupted"
)

type host struct {
	m       machine
	g       *guest
	man     Manifest
	logf    func(string, ...any)
	stopReq chan struct{}
	once    sync.Once
	sync    *syncer

	mu     sync.Mutex
	status HostStatus
}

// RunHost is the helper process (care-clinic --vm-host). It boots the VM,
// exposes Docker to this computer and serves the control socket until asked to
// stop, then shuts the guest down cleanly.
func RunHost() int {
	if !Supported() {
		fmt.Fprintln(os.Stderr, errUnsupported)
		return 2
	}
	_ = os.MkdirAll(StateDir(), 0o700)
	lf, err := applog.NewStream(HostLog())
	if err == nil {
		log.SetOutput(lf)
	} else {
		fmt.Fprintf(os.Stderr, "engine logging unavailable: %v\n", err)
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	logf := log.Printf
	if err := runHost(logf); err != nil {
		logf("engine: %v", err)
		return 1
	}
	return 0
}

func runHost(logf func(string, ...any)) error {
	if err := os.MkdirAll(runDir(), 0o700); err != nil {
		return err
	}
	if c := newClient(); c.alive(context.Background()) {
		return errors.New("another engine helper is already running")
	}
	_ = os.Remove(controlSocket())
	ctl, err := net.Listen("unix", controlSocket())
	if err != nil {
		return fmt.Errorf("control socket: %w", err)
	}
	defer func() { _ = os.Remove(controlSocket()) }()

	man, err := Installed()
	if err != nil {
		return err
	}
	cfg := LoadConfig()
	if err := prepareDataDisk(man, cfg.DiskGB); err != nil {
		return fmt.Errorf("data disk: %w", err)
	}
	logf("engine: booting %s (%d CPUs, %d MB, %d GB disk)", man.Version, cfg.CPUs, cfg.MemoryMB, cfg.DiskGB)
	m, err := boot(bootOptions{manifest: man, config: cfg, logf: logf})
	if err != nil {
		return fmt.Errorf("start VM: %w", err)
	}
	h := &host{
		m: m, man: man, logf: logf, stopReq: make(chan struct{}),
		g:      newGuest(func(ctx context.Context) (net.Conn, error) { return m.dial(ctx, vsockControl) }),
		status: HostStatus{Running: true, Version: man.Version, PID: os.Getpid(), Logs: LogsConnecting},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.collectLogs(ctx)

	srv := &http.Server{Handler: h.routes(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ctl) }()
	defer func() { _ = srv.Close() }()

	if err := h.waitReady(ctx, 4*time.Minute); err != nil {
		h.setError(err)
		_ = m.kill()
		return err
	}
	if err := h.attach(ctx); err != nil {
		h.setError(err)
		h.shutdown()
		return err
	}
	logf("engine: ready")
	go h.housekeeping(ctx)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-h.stopReq:
		logf("engine: stop requested")
	case s := <-sig:
		logf("engine: %s", s)
	case <-m.done():
		logf("engine: VM stopped on its own")
		return fmt.Errorf("the VM stopped unexpectedly; see %s", ConsoleLog())
	}
	cancel()
	h.shutdown()
	return nil
}

func (h *host) setLogs(state string) {
	h.mu.Lock()
	h.status.Logs = state
	h.mu.Unlock()
}

func (h *host) setError(err error) {
	h.mu.Lock()
	h.status.Error = err.Error()
	h.mu.Unlock()
}

func (h *host) waitReady(ctx context.Context, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	var last error
	for time.Now().Before(deadline) {
		select {
		case <-h.m.done():
			return fmt.Errorf("the VM stopped while starting; see %s", ConsoleLog())
		default:
		}
		c, cancel := context.WithTimeout(ctx, 3*time.Second)
		s, err := h.g.status(c)
		cancel()
		if err == nil && s.Docker {
			_ = h.g.setTime(ctx, time.Now())
			h.mu.Lock()
			h.status.Docker = true
			h.mu.Unlock()
			return nil
		}
		if err == nil {
			err = errors.New("Docker is still starting")
		}
		last = err
		time.Sleep(time.Second)
	}
	return fmt.Errorf("the engine did not become ready: %v", last)
}

// attach makes the engine usable from this computer: folder shares and
// sockets on macOS, file sync on Windows.
func (h *host) attach(ctx context.Context) error {
	if runtime.GOOS == "windows" {
		h.sync = newSyncer(h.g, h.logf)
		h.mu.Lock()
		h.status.WebPorts = []int{80, 443}
		h.mu.Unlock()
		return nil
	}
	for _, s := range darwinShares() {
		if err := h.g.mount(ctx, s.tag, s.guest); err != nil {
			return fmt.Errorf("share %s: %w", s.host, err)
		}
	}
	_ = os.Remove(dockerSocket())
	ln, err := net.Listen("unix", dockerSocket())
	if err != nil {
		return fmt.Errorf("docker socket: %w", err)
	}
	_ = os.Chmod(dockerSocket(), 0o600)
	go forward(ctx, ln, func(c context.Context) (net.Conn, error) { return h.m.dial(c, vsockDocker) }, h.logf)
	// The clinic's web ports. Binding below 1024 needs no privilege on
	// macOS 10.14+; if something else holds a port, the clinic reports it.
	for _, p := range []uint32{vsockHTTP, vsockHTTPS} {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", p))
		if err != nil {
			h.logf("engine: port %d: %v", p, err)
			continue
		}
		h.mu.Lock()
		h.status.WebPorts = append(h.status.WebPorts, int(p))
		h.mu.Unlock()
		go forward(ctx, ln, func(c context.Context) (net.Conn, error) { return h.m.dial(c, p) }, h.logf)
	}
	return nil
}

// housekeeping keeps the guest clock right after the computer sleeps, keeps
// disk numbers fresh and, on Windows, keeps shared folders in step.
func (h *host) housekeeping(ctx context.Context) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	last := time.Now()
	for {
		h.refresh(ctx)
		if h.sync != nil {
			if err := h.sync.run(ctx, nil); err != nil {
				h.logf("sync: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			// Wall-clock jumps mean the host slept; the guest clock froze.
			if now.Sub(last) > time.Minute || now.Round(0).Sub(last.Round(0)) > time.Minute {
				_ = h.g.setTime(ctx, now)
			}
			last = now
		}
	}
}

func (h *host) refresh(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	s, err := h.g.status(c)
	free, total, derr := h.g.df(c)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status.Docker = err == nil && s.Docker
	if derr == nil {
		h.status.DiskFree, h.status.DiskTotal = free, total
	}
}

func (h *host) shutdown() {
	h.logf("engine: shutting down guest")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := h.g.shutdown(ctx)
	cancel()
	if err != nil {
		h.logf("engine: shutdown request: %v", err)
	}
	select {
	case <-h.m.done():
		h.logf("engine: stopped")
	case <-time.After(90 * time.Second):
		h.logf("engine: guest did not stop in time; powering off")
		_ = h.m.kill()
	}
}

func (h *host) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		s := h.status
		h.mu.Unlock()
		writeJSON(w, s)
	})
	mux.HandleFunc("POST /stop", func(w http.ResponseWriter, r *http.Request) {
		h.once.Do(func() { close(h.stopReq) })
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /sync", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Paths []string `json:"paths"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if h.sync == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err := h.sync.run(r.Context(), in.Paths); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /journal", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if err := h.g.journal(r.Context(), w); err != nil {
			_, _ = fmt.Fprintln(w, err)
		}
	})
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

type share struct{ tag, host, guest string }

// darwinShares are the macOS folders the VM can see, at the same paths, so
// bind mounts from the install and backup folders work unchanged.
func darwinShares() []share {
	home, _ := os.UserHomeDir()
	out := []share{{tag: "care-home", host: home, guest: filepath.ToSlash(home)}}
	if fi, err := os.Stat("/Volumes"); err == nil && fi.IsDir() {
		out = append(out, share{tag: "care-volumes", host: "/Volumes", guest: "/Volumes"})
	}
	return out
}
