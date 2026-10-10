package vm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type client struct{ http *http.Client }

func newClient() *client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", controlSocket())
		},
		DisableKeepAlives: true,
	}
	return &client{http: &http.Client{Transport: tr}}
}

func (c *client) do(ctx context.Context, method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://engine"+path, r)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("CARE engine: %s", strings.TrimSpace(string(msg)))
	}
	if w, ok := out.(io.Writer); ok {
		_, err = io.Copy(w, resp.Body)
		return err
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *client) alive(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return c.do(ctx, http.MethodGet, "/status", nil, nil) == nil
}

// Status asks the helper how the engine is doing. Running is false when the
// helper is not up.
func Status(ctx context.Context) HostStatus {
	var s HostStatus
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := newClient().do(ctx, http.MethodGet, "/status", nil, &s); err != nil {
		return HostStatus{}
	}
	return s
}

// Ensure starts the engine if needed and waits until Docker answers.
func Ensure(ctx context.Context, logf func(string)) error {
	if !Supported() {
		return nil
	}
	if s := Status(ctx); s.Running && s.Docker {
		return nil
	}
	if _, err := Installed(); err != nil {
		return err
	}
	if !ToolsInstalled() {
		return errors.New("the CARE engine tools are missing; reinstall the CARE engine")
	}
	c := newClient()
	if !c.alive(ctx) {
		if logf != nil {
			logf("Starting the CARE engine…")
		}
		if err := spawnHost(); err != nil {
			return fmt.Errorf("start CARE engine: %w", err)
		}
	}
	deadline := time.Now().Add(5 * time.Minute)
	seen := false
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		s := Status(ctx)
		switch {
		case s.Running && s.Docker:
			return nil
		case s.Error != "":
			return fmt.Errorf("CARE engine: %s", s.Error)
		case s.Running:
			seen = true
		case seen:
			return fmt.Errorf("the CARE engine stopped while starting: %s", lastLogLine())
		}
		time.Sleep(time.Second)
	}
	return errors.New("the CARE engine took too long to start")
}

// Stop shuts the VM down and waits for the helper to exit.
func Stop(ctx context.Context) error {
	if !Supported() {
		return nil
	}
	c := newClient()
	if !c.alive(ctx) {
		return nil
	}
	if err := c.do(ctx, http.MethodPost, "/stop", nil, nil); err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if !c.alive(ctx) {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("the CARE engine did not stop in time")
}

// Sync copies the named host paths (and the folders already being kept in
// step) into the engine. Only Windows needs it; elsewhere it does nothing.
func Sync(ctx context.Context, paths []string) error {
	if !needsSync() {
		return nil
	}
	return newClient().do(ctx, http.MethodPost, "/sync", map[string]any{"paths": paths}, nil)
}

// WriteDiagnostics copies the engine's logs and the guest journal to w.
func WriteDiagnostics(ctx context.Context, w io.Writer) {
	for _, p := range []string{HostLog(), ConsoleLog()} {
		_, _ = fmt.Fprintf(w, "===== %s =====\n", p)
		if b, err := os.ReadFile(p); err == nil {
			_, _ = w.Write(tail(b, 256<<10))
		} else {
			_, _ = fmt.Fprintln(w, err)
		}
	}
	_, _ = fmt.Fprintln(w, "===== guest journal =====")
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := newClient().do(ctx, http.MethodGet, "/journal", nil, w); err != nil {
		_, _ = fmt.Fprintln(w, err)
	}
}

func tail(b []byte, n int) []byte {
	if len(b) > n {
		return b[len(b)-n:]
	}
	return b
}

func lastLogLine() string {
	b, err := os.ReadFile(HostLog())
	if err != nil {
		return "see the engine log"
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return lines[len(lines)-1]
}

func spawnHost() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(StateDir(), 0o700); err != nil {
		return err
	}
	cmd := exec.Command(self, HostFlag)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
