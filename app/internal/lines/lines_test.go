package lines

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func digest(c byte) string { return "@sha256:" + strings.Repeat(string(c), 64) }

func sample() Manifest {
	line := func(id string, c byte) Line {
		return Line{
			ID: id,
			Backend: Image{
				Commit:  strings.Repeat(string(c), 40),
				Image:   "ghcr.io/ohcnetwork/care-clinic-backend" + digest(c),
				Builder: "ghcr.io/ohcnetwork/care-clinic-backend-builder" + digest(c),
			},
			Frontend: Image{
				Commit:      strings.Repeat(string(c), 40),
				Image:       "ghcr.io/ohcnetwork/care-clinic-frontend" + digest(c),
				Builder:     "ghcr.io/ohcnetwork/care-clinic-frontend-builder" + digest(c),
				RuntimeKeys: []string{"REACT_APP_TITLE"},
				BuildEnv:    map[string]string{"REACT_MFE_REGISTERED_COMPONENTS": "UserDashboard"},
			},
		}
	}
	return Manifest{Schema: Schema, Recommended: "2025.2", Lines: []Line{line("2025.1", 'a'), line("2025.2", 'b')}}
}

func encode(t *testing.T, m Manifest) []byte {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseAcceptsAValidManifest(t *testing.T) {
	m, err := Parse(encode(t, sample()))
	if err != nil {
		t.Fatal(err)
	}
	if l, ok := m.Line("2025.1"); !ok || l.Backend.Commit == "" {
		t.Fatal("line lookup failed")
	}
	if newer := m.Newer("2025.1"); len(newer) != 1 || newer[0].ID != "2025.2" {
		t.Fatalf("newer lines = %+v", newer)
	}
	if !m.IsNewer("2025.1", "2025.2") || m.IsNewer("2025.2", "2025.1") || m.IsNewer("missing", "2025.2") {
		t.Fatal("line ordering is wrong")
	}
}

func TestParseRejectsUnsafeManifests(t *testing.T) {
	for name, mutate := range map[string]func(*Manifest){
		"schema":            func(m *Manifest) { m.Schema = 2 },
		"no lines":          func(m *Manifest) { m.Lines = nil },
		"duplicate":         func(m *Manifest) { m.Lines[1].ID = m.Lines[0].ID },
		"bad id":            func(m *Manifest) { m.Lines[0].ID = "../x" },
		"tag image":         func(m *Manifest) { m.Lines[0].Backend.Image = "ghcr.io/x/care:latest" },
		"tag builder":       func(m *Manifest) { m.Lines[0].Frontend.Builder = "ghcr.io/x/fe:1" },
		"short commit":      func(m *Manifest) { m.Lines[0].Backend.Commit = "abc" },
		"no runtime keys":   func(m *Manifest) { m.Lines[0].Frontend.RuntimeKeys = nil },
		"bad key":           func(m *Manifest) { m.Lines[0].Frontend.RuntimeKeys = []string{"bad key"} },
		"runtime and build": func(m *Manifest) { m.Lines[0].Frontend.BuildEnv["REACT_APP_TITLE"] = "x" },
		"bad min version":   func(m *Manifest) { m.Lines[0].MinAppVersion = "1.0" },
		"bad date":          func(m *Manifest) { m.Lines[0].SupportedUntil = "2025-13-01" },
		"recommended":       func(m *Manifest) { m.Recommended = "nope" },
	} {
		t.Run(name, func(t *testing.T) {
			m := sample()
			mutate(&m)
			if _, err := Parse(encode(t, m)); err == nil {
				t.Fatal("unsafe manifest was accepted")
			}
		})
	}
	if _, err := Parse([]byte(`{"schema":1,"lines":[],"extra":true}`)); err == nil {
		t.Fatal("unknown fields were accepted")
	}
}

func TestSupportsApp(t *testing.T) {
	l := Line{ID: "x", MinAppVersion: "0.2.0", MaxAppVersion: "0.4.0"}
	for version, ok := range map[string]bool{
		"0.1.9": false, "0.2.0": true, "0.2.0-dev": true, "0.4.0": true, "0.4.1": false, "1.0.0": false,
	} {
		if err := l.SupportsApp(version); (err == nil) != ok {
			t.Errorf("SupportsApp(%s) = %v", version, err)
		}
	}
}

func TestExpired(t *testing.T) {
	l := Line{SupportedUntil: "2025-01-31"}
	if l.Expired(time.Date(2025, 1, 31, 23, 0, 0, 0, time.UTC)) {
		t.Fatal("expired on its last day")
	}
	if !l.Expired(time.Date(2025, 2, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("not expired after its last day")
	}
	if (Line{}).Expired(time.Now()) {
		t.Fatal("open-ended line expired")
	}
}

func TestLoadFallsBackToTheCachedManifest(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(t.TempDir(), "lines.json")
	if err := os.WriteFile(src, encode(t, sample()), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, src); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir, src)
	if err != nil || len(m.Lines) != 2 {
		t.Fatalf("cached manifest was not used: %v", err)
	}
	if _, err := Fetch("", src); err == nil {
		t.Fatal("fetch of a missing manifest succeeded")
	}
}

func TestDownloadRejectsInsecureRemoteManifests(t *testing.T) {
	for _, src := range []string{"http://example.com/lines.json", "ftp://example.com/lines.json", ""} {
		if _, err := download(src); err == nil {
			t.Errorf("%q was accepted", src)
		}
	}
}

func TestSourceHonoursTheOverride(t *testing.T) {
	t.Setenv(OverrideEnv, "")
	if Source("https://pinned") != "https://pinned" {
		t.Fatal("pinned source ignored")
	}
	t.Setenv(OverrideEnv, " /tmp/lines.json ")
	if Source("https://pinned") != "/tmp/lines.json" {
		t.Fatal("override ignored")
	}
}
