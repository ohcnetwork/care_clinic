package vm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// guest talks to the agent's control API inside the VM.
type guest struct {
	http *http.Client
}

func newGuest(dial func(ctx context.Context) (net.Conn, error)) *guest {
	tr := &http.Transport{
		DialContext:         func(ctx context.Context, _, _ string) (net.Conn, error) { return dial(ctx) },
		MaxIdleConns:        2,
		IdleConnTimeout:     30 * time.Second,
		DisableCompression:  true,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &guest{http: &http.Client{Transport: tr}}
}

type guestStatus struct {
	Docker  bool   `json:"docker"`
	Version string `json:"version"`
	Uptime  int64  `json:"uptime"`
}

type guestEntry struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	MtimeNs int64  `json:"mtime_ns"`
	Mode    uint32 `json:"mode"`
}

func (g *guest) do(ctx context.Context, method, path string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, "http://agent"+path, body)
	if err != nil {
		return err
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("engine agent %s %s: %s %s", method, path, resp.Status, bytes.TrimSpace(msg))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if w, ok := out.(io.Writer); ok {
		_, err = io.Copy(w, resp.Body)
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func jsonBody(v any) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

func (g *guest) status(ctx context.Context) (guestStatus, error) {
	var s guestStatus
	err := g.do(ctx, http.MethodGet, "/v1/status", nil, &s)
	return s, err
}

func (g *guest) mount(ctx context.Context, tag, path string) error {
	return g.do(ctx, http.MethodPost, "/v1/mount", jsonBody(map[string]string{"tag": tag, "path": path}), nil)
}

func (g *guest) setTime(ctx context.Context, t time.Time) error {
	return g.do(ctx, http.MethodPost, "/v1/time", jsonBody(map[string]int64{"unix_nano": t.UnixNano()}), nil)
}

func (g *guest) shutdown(ctx context.Context) error {
	return g.do(ctx, http.MethodPost, "/v1/shutdown", nil, nil)
}

func (g *guest) df(ctx context.Context) (free, total uint64, err error) {
	var out struct{ Free, Total uint64 }
	err = g.do(ctx, http.MethodGet, "/v1/df", nil, &out)
	return out.Free, out.Total, err
}

func (g *guest) journal(ctx context.Context, w io.Writer) error {
	return g.do(ctx, http.MethodGet, "/v1/journal", nil, w)
}

func (g *guest) list(ctx context.Context, root string) ([]guestEntry, error) {
	var out []guestEntry
	err := g.do(ctx, http.MethodGet, "/v1/fs/list?root="+url.QueryEscape(root), nil, &out)
	return out, err
}

func (g *guest) get(ctx context.Context, p string) (io.ReadCloser, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://agent/v1/fs/file?path="+url.QueryEscape(p), nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, 0, fmt.Errorf("engine agent GET %s: %s", p, resp.Status)
	}
	mt, _ := strconv.ParseInt(resp.Header.Get("X-Mtime-Ns"), 10, 64)
	return resp.Body, mt, nil
}

func (g *guest) put(ctx context.Context, p string, r io.Reader, mode uint32, mtime time.Time) error {
	q := url.Values{"path": {p}, "mode": {strconv.FormatUint(uint64(mode&0o777), 8)}, "mtime_ns": {strconv.FormatInt(mtime.UnixNano(), 10)}}
	return g.do(ctx, http.MethodPut, "/v1/fs/file?"+q.Encode(), r, nil)
}

func (g *guest) remove(ctx context.Context, p string) error {
	return g.do(ctx, http.MethodDelete, "/v1/fs/file?path="+url.QueryEscape(p), nil, nil)
}
