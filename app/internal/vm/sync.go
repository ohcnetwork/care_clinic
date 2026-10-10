package vm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// On Windows the VM cannot see host folders, so the helper keeps a copy of the
// install and backup folders inside it. Each pass compares both sides with
// what was last synced (size + modification time): a side that changed wins,
// a file removed on one unchanged side is removed on the other, and if both
// changed the computer's copy wins.

type fileState struct {
	Size    int64 `json:"size"`
	MtimeUs int64 `json:"mtime_us"`
}

func (a fileState) same(b fileState) bool { return a == b }

// guestFS is the part of the agent client the syncer uses (faked in tests).
type guestFS interface {
	list(ctx context.Context, root string) ([]guestEntry, error)
	get(ctx context.Context, p string) (io.ReadCloser, int64, error)
	put(ctx context.Context, p string, r io.Reader, mode uint32, mtime time.Time) error
	remove(ctx context.Context, p string) error
}

type syncer struct {
	g      guestFS
	logf   func(string, ...any)
	state  string
	roots  func() []string
	toGst  func(string) string
	mu     sync.Mutex
	skipFn func(rel string) bool
}

func newSyncer(g guestFS, logf func(string, ...any)) *syncer {
	return &syncer{
		g: g, logf: logf,
		state:  filepath.Join(StateDir(), "sync"),
		roots:  SyncRoots,
		toGst:  GuestPath,
		skipFn: skipSync,
	}
}

func skipSync(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		switch part {
		case ".git", "node_modules", "__pycache__":
			return true
		}
	}
	return strings.HasSuffix(rel, ".tmp")
}

func syncRootsFile() string { return filepath.Join(StateDir(), "sync-roots.json") }

// SetSyncRoots records the folders the engine must mirror (Windows only).
func SetSyncRoots(roots []string) error {
	if !needsSync() {
		return nil
	}
	if err := os.MkdirAll(StateDir(), 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(roots)
	if old, err := os.ReadFile(syncRootsFile()); err == nil && bytes.Equal(old, b) {
		return nil
	}
	return os.WriteFile(syncRootsFile(), b, 0o600)
}

func SyncRoots() []string {
	var roots []string
	if b, err := os.ReadFile(syncRootsFile()); err == nil {
		_ = json.Unmarshal(b, &roots)
	}
	return roots
}

// run syncs every root, then pushes the extra paths (files or folders that a
// docker command is about to mount).
func (s *syncer) run(ctx context.Context, extra []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	for _, r := range s.roots() {
		if r == "" {
			continue
		}
		if err := s.syncRoot(ctx, r); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r, err))
		}
	}
	for _, p := range extra {
		if under(p, s.roots()) {
			continue
		}
		if err := s.push(ctx, p); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
		}
	}
	return errors.Join(errs...)
}

func under(p string, roots []string) bool {
	cp := strings.ToLower(filepath.Clean(p))
	for _, r := range roots {
		cr := strings.ToLower(filepath.Clean(r))
		if r != "" && (cp == cr || strings.HasPrefix(cp, cr+string(filepath.Separator))) {
			return true
		}
	}
	return false
}

func (s *syncer) statePath(root string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(root))))
	return filepath.Join(s.state, hex.EncodeToString(sum[:8])+".json")
}

func (s *syncer) loadState(root string) map[string]fileState {
	m := map[string]fileState{}
	if b, err := os.ReadFile(s.statePath(root)); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func (s *syncer) saveState(root string, m map[string]fileState) error {
	if err := os.MkdirAll(s.state, 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(m)
	tmp := s.statePath(root) + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.statePath(root))
}

func hostFiles(root string, skip func(string) bool) (map[string]fileState, error) {
	out := map[string]fileState{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel != "." && skip(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out[rel] = fileState{Size: info.Size(), MtimeUs: info.ModTime().UnixMicro()}
		return nil
	})
	return out, err
}

func (s *syncer) syncRoot(ctx context.Context, root string) error {
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return nil
	}
	groot := s.toGst(root)
	host, err := hostFiles(root, s.skipFn)
	if err != nil {
		return err
	}
	entries, err := s.g.list(ctx, groot)
	if err != nil {
		return err
	}
	guest := map[string]fileState{}
	for _, e := range entries {
		if !s.skipFn(e.Path) {
			guest[e.Path] = fileState{Size: e.Size, MtimeUs: e.MtimeNs / 1000}
		}
	}
	last := s.loadState(root)
	next := map[string]fileState{}
	names := map[string]bool{}
	for k := range host {
		names[k] = true
	}
	for k := range guest {
		names[k] = true
	}
	for k := range last {
		names[k] = true
	}
	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var errs []error
	for _, rel := range keys {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, hok := host[rel]
		g, gok := guest[rel]
		l, lok := last[rel]
		hChanged := hok != lok || (hok && !h.same(l))
		gChanged := gok != lok || (gok && !g.same(l))
		hp := filepath.Join(root, filepath.FromSlash(rel))
		gp := path.Join(groot, rel)
		var err error
		switch {
		case hok && gok && h.same(g):
			next[rel] = h
		case hChanged && (!gChanged || hok):
			if hok {
				if err = s.pushFile(ctx, hp, gp); err == nil {
					next[rel] = h
				}
			} else if gok {
				err = s.g.remove(ctx, gp)
			}
		case gChanged:
			if gok {
				var st fileState
				if st, err = s.pullFile(ctx, gp, hp); err == nil {
					next[rel] = st
				}
			} else if hok {
				if err = os.Remove(hp); errors.Is(err, fs.ErrNotExist) {
					err = nil
				}
			}
		default:
			if hok {
				next[rel] = h
			}
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", rel, err))
			if lok {
				next[rel] = l
			}
		}
	}
	if err := s.saveState(root, next); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func modeFor(name string) uint32 {
	if strings.HasSuffix(name, ".sh") {
		return 0o755
	}
	return 0o644
}

func (s *syncer) pushFile(ctx context.Context, hp, gp string) error {
	f, err := os.Open(hp)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	return s.g.put(ctx, gp, f, modeFor(hp), info.ModTime())
}

func (s *syncer) pullFile(ctx context.Context, gp, hp string) (fileState, error) {
	rc, mtimeNs, err := s.g.get(ctx, gp)
	if err != nil {
		return fileState{}, err
	}
	defer func() { _ = rc.Close() }()
	if err := os.MkdirAll(filepath.Dir(hp), 0o755); err != nil {
		return fileState{}, err
	}
	tmp := hp + ".care-sync.tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return fileState{}, err
	}
	n, err := io.Copy(out, rc)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return fileState{}, err
	}
	mt := time.Unix(0, mtimeNs)
	_ = os.Chtimes(tmp, mt, mt)
	_ = os.Remove(hp)
	if err := os.Rename(tmp, hp); err != nil {
		return fileState{}, err
	}
	return fileState{Size: n, MtimeUs: mt.UnixMicro()}, nil
}

// push copies a file or folder outside the synced roots into the engine.
func (s *syncer) push(ctx context.Context, p string) error {
	fi, err := os.Stat(p)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return s.pushFile(ctx, p, s.toGst(p))
	}
	files, err := hostFiles(p, s.skipFn)
	if err != nil {
		return err
	}
	gl, err := s.g.list(ctx, s.toGst(p))
	if err != nil {
		return err
	}
	have := map[string]fileState{}
	for _, e := range gl {
		have[e.Path] = fileState{Size: e.Size, MtimeUs: e.MtimeNs / 1000}
	}
	for rel, st := range files {
		if g, ok := have[rel]; ok && g.same(st) {
			continue
		}
		if err := s.pushFile(ctx, filepath.Join(p, filepath.FromSlash(rel)), path.Join(s.toGst(p), rel)); err != nil {
			return err
		}
	}
	return nil
}
