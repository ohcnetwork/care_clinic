package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	root := t.TempDir()
	a := newAPI(filepath.Join(root, "missing.sock"))
	a.root = root
	srv := httptest.NewServer(a.routes())
	t.Cleanup(srv.Close)
	return srv, root
}

func do(t *testing.T, method, u string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestFileRoundTripKeepsMtimeAndMode(t *testing.T) {
	srv, root := testServer(t)
	mtime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).UnixNano()
	q := url.Values{"path": {"/c/Users/a b/install/Caddyfile"}, "mode": {"600"}, "mtime_ns": {itoa(mtime)}}
	if r := do(t, http.MethodPut, srv.URL+"/v1/fs/file?"+q.Encode(), strings.NewReader("hello")); r.StatusCode != http.StatusNoContent {
		t.Fatalf("put: %s", r.Status)
	}
	info, err := os.Stat(filepath.Join(root, "c", "Users", "a b", "install", "Caddyfile"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 || info.ModTime().UnixNano() != mtime {
		t.Fatalf("mode %v mtime %d", info.Mode(), info.ModTime().UnixNano())
	}

	r := do(t, http.MethodGet, srv.URL+"/v1/fs/list?root="+url.QueryEscape("/c/Users/a b/install"), nil)
	var entries []Entry
	if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Path != "Caddyfile" || entries[0].Size != 5 || entries[0].MtimeNs != mtime {
		t.Fatalf("entries %+v", entries)
	}

	r = do(t, http.MethodGet, srv.URL+"/v1/fs/file?path="+url.QueryEscape("/c/Users/a b/install/Caddyfile"), nil)
	b, _ := io.ReadAll(r.Body)
	if string(b) != "hello" || r.Header.Get("X-Mtime-Ns") != itoa(mtime) {
		t.Fatalf("get %q %s", b, r.Header.Get("X-Mtime-Ns"))
	}

	if r := do(t, http.MethodDelete, srv.URL+"/v1/fs/file?path="+url.QueryEscape("/c/Users/a b/install/Caddyfile"), nil); r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %s", r.Status)
	}
	if _, err := os.Stat(filepath.Join(root, "c", "Users", "a b", "install", "Caddyfile")); !os.IsNotExist(err) {
		t.Fatalf("still there: %v", err)
	}
}

func TestListMissingRootIsEmpty(t *testing.T) {
	srv, _ := testServer(t)
	r := do(t, http.MethodGet, srv.URL+"/v1/fs/list?root=/nope", nil)
	b, _ := io.ReadAll(r.Body)
	if r.StatusCode != http.StatusOK || strings.TrimSpace(string(b)) != "[]" {
		t.Fatalf("%s %s", r.Status, b)
	}
}

func TestPathsStayInsideRoot(t *testing.T) {
	srv, root := testServer(t)
	for _, p := range []string{"relative/file", "/", "/../../etc/passwd"} {
		q := url.Values{"path": {p}}
		r := do(t, http.MethodPut, srv.URL+"/v1/fs/file?"+q.Encode(), strings.NewReader("x"))
		if p == "/../../etc/passwd" {
			// Cleaned to /etc/passwd under the root, never outside it.
			if _, err := os.Stat(filepath.Join(root, "etc", "passwd")); err != nil || r.StatusCode != http.StatusNoContent {
				t.Fatalf("%s: %s %v", p, r.Status, err)
			}
			continue
		}
		if r.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: %s", p, r.Status)
		}
	}
}

func TestStatusWithoutDocker(t *testing.T) {
	srv, _ := testServer(t)
	r := do(t, http.MethodGet, srv.URL+"/v1/status", nil)
	var s Status
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil || s.Docker {
		t.Fatalf("%+v %v", s, err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
