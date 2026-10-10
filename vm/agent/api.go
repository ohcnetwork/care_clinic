package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type api struct {
	docker  string
	started time.Time
	// root confines the file API; "/" in the guest, a temp dir in tests.
	root string
}

func newAPI(dockerSock string) *api {
	return &api{docker: dockerSock, started: time.Now(), root: "/"}
}

// Entry describes one file in a listing. Paths are slash-separated and
// relative to the listed root.
type Entry struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	MtimeNs int64  `json:"mtime_ns"`
	Mode    uint32 `json:"mode"`
}

type Status struct {
	Docker  bool   `json:"docker"`
	Version string `json:"version"`
	Uptime  int64  `json:"uptime"`
}

func (a *api) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", a.status)
	mux.HandleFunc("POST /v1/mount", a.mount)
	mux.HandleFunc("POST /v1/time", a.setTime)
	mux.HandleFunc("POST /v1/shutdown", a.shutdown)
	mux.HandleFunc("GET /v1/df", a.df)
	mux.HandleFunc("GET /v1/journal", a.journal)
	mux.HandleFunc("GET /v1/logs", a.logs)
	mux.HandleFunc("GET /v1/fs/list", a.list)
	mux.HandleFunc("GET /v1/fs/file", a.getFile)
	mux.HandleFunc("PUT /v1/fs/file", a.putFile)
	mux.HandleFunc("DELETE /v1/fs/file", a.deleteFile)
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (a *api) status(w http.ResponseWriter, r *http.Request) {
	s := Status{Uptime: int64(time.Since(a.started).Seconds())}
	client := http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", a.docker)
		}},
	}
	if resp, err := client.Get("http://docker/version"); err == nil {
		var v struct{ Version string }
		_ = json.NewDecoder(resp.Body).Decode(&v)
		_ = resp.Body.Close()
		s.Docker = resp.StatusCode == http.StatusOK
		s.Version = v.Version
	}
	writeJSON(w, s)
}

func (a *api) mount(w http.ResponseWriter, r *http.Request) {
	var in struct{ Tag, Path string }
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&in); err != nil || in.Tag == "" || !path.IsAbs(in.Path) {
		http.Error(w, "tag and absolute path required", http.StatusBadRequest)
		return
	}
	if err := mountShare(in.Tag, path.Clean(in.Path)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) setTime(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UnixNano int64 `json:"unix_nano"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&in); err != nil || in.UnixNano <= 0 {
		http.Error(w, "unix_nano required", http.StatusBadRequest)
		return
	}
	// Only step the clock when it has drifted, e.g. after the host slept.
	if d := time.Since(time.Unix(0, in.UnixNano)); d > 2*time.Second || d < -2*time.Second {
		if err := setClock(time.Unix(0, in.UnixNano)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) shutdown(w http.ResponseWriter, r *http.Request) {
	// Let systemd stop dockerd (and with it the containers) cleanly; reply first.
	cmd := exec.Command("systemd-run", "--unit=care-engine-poweroff", "--on-active=1s",
		"systemctl", "poweroff", "--no-block")
	if err := cmd.Run(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (a *api) df(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		p = "/var/lib"
	}
	free, total, err := diskUsage(p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]uint64{"free": free, "total": total})
}

func (a *api) journal(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "journalctl", "-b", "--no-pager", "-n", "3000")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	cmd.Stdout, cmd.Stderr = w, w
	_ = cmd.Run()
}

// resolve maps a guest path from the request into the file API root and
// rejects anything that is not absolute or that would climb out of it.
func (a *api) resolve(p string) (string, error) {
	if !path.IsAbs(p) || strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("absolute path required")
	}
	clean := path.Clean(p)
	if clean == "/" {
		return "", fmt.Errorf("refusing to work on /")
	}
	return filepath.Join(a.root, filepath.FromSlash(clean)), nil
}

func (a *api) list(w http.ResponseWriter, r *http.Request) {
	root, err := a.resolve(r.URL.Query().Get("root"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	entries := []Entry{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && p == root {
				return fs.SkipAll
			}
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		entries = append(entries, Entry{Path: filepath.ToSlash(rel), Size: info.Size(),
			MtimeNs: info.ModTime().UnixNano(), Mode: uint32(info.Mode().Perm())})
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, entries)
}

func (a *api) getFile(w http.ResponseWriter, r *http.Request) {
	p, err := a.resolve(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, err := os.Open(p)
	if err != nil {
		code := http.StatusInternalServerError
		if os.IsNotExist(err) {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.Error(w, "not a regular file", http.StatusBadRequest)
		return
	}
	w.Header().Set("X-Mtime-Ns", strconv.FormatInt(info.ModTime().UnixNano(), 10))
	w.Header().Set("X-Mode", strconv.FormatUint(uint64(info.Mode().Perm()), 8))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	_, _ = io.Copy(w, f)
}

// putFile writes through a temp file and rename, then stamps the host's
// mtime so the next listing compares equal.
func (a *api) putFile(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p, err := a.resolve(q.Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mode := os.FileMode(0o644)
	if m, err := strconv.ParseUint(q.Get("mode"), 8, 32); err == nil {
		mode = os.FileMode(m) & os.ModePerm
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".care-sync-*")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = io.Copy(tmp, r.Body)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), mode)
	}
	if err == nil {
		if ns, perr := strconv.ParseInt(q.Get("mtime_ns"), 10, 64); perr == nil && ns > 0 {
			t := time.Unix(0, ns)
			err = os.Chtimes(tmp.Name(), t, t)
		}
	}
	if err == nil {
		err = os.Rename(tmp.Name(), p)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) deleteFile(w http.ResponseWriter, r *http.Request) {
	p, err := a.resolve(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
