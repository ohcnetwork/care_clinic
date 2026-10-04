package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
)

func TestNewerVersion(t *testing.T) {
	for _, tt := range []struct {
		current, candidate string
		want               bool
	}{
		{"0.1.0", "0.1.1", true},
		{"0.1.0", "0.2.0", true},
		{"0.1.0", "1.0.0", true},
		{"0.1.0", "0.1.0", false},
		{"0.2.0", "0.1.9", false},
		{"1.0.0", "0.9.9", false},
		{"0.9.0", "0.10.0", true},
		{"0.10.0", "0.9.0", false},
		{"0.1.0-dev", "0.1.0", true},
		{"0.1.0-dev", "0.0.9", false},
		{"0.1.0", "not-a-version", false},
	} {
		if got := newerVersion(tt.current, tt.candidate); got != tt.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", tt.current, tt.candidate, got, tt.want)
		}
	}
}

func TestPlatformAssetPicksOneInstaller(t *testing.T) {
	rel := ghRelease{Assets: []ghAsset{
		{Name: "SHA256SUMS", URL: "https://example.invalid/SHA256SUMS"},
		{Name: "release-manifest.json", URL: "https://example.invalid/manifest"},
		{Name: "CARE-Clinic-1.2.3-macos.dmg", URL: "https://example.invalid/dmg"},
		{Name: "CARE-Clinic-1.2.3-windows-amd64-setup.exe", URL: "https://example.invalid/exe"},
	}}
	asset, ok := platformAsset(rel)
	if !ok {
		t.Skip("no installer is published for this platform")
	}
	if asset.URL == "" || asset.Name == "SHA256SUMS" {
		t.Fatalf("picked %q as the installer", asset.Name)
	}
	if _, ok := findAsset(rel, func(name string) bool { return name == "SHA256SUMS" }); !ok {
		t.Fatal("checksums were not found alongside the installer")
	}
	empty := ghRelease{Assets: []ghAsset{{Name: "CARE-Clinic-1.2.3-macos.dmg"}}}
	if _, ok := platformAsset(empty); ok {
		t.Fatal("an asset with no URL was accepted")
	}
}

func TestDownloadVerifiedRetriesThenRejectsDamagedInstallers(t *testing.T) {
	good := []byte("CARE Clinic installer")
	digest := sha256.Sum256(good)
	want := hex.EncodeToString(digest[:])
	for _, tc := range []struct {
		name      string
		bodies    []string
		requests  int32
		installed bool
	}{
		{"intact", []string{string(good)}, 1, true},
		{"damaged once", []string{"truncated", string(good)}, 2, true},
		{"damaged twice", []string{"truncated", "corrupted"}, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("TMPDIR", tmp)
			t.Setenv("TMP", tmp)
			t.Setenv("TEMP", tmp)
			t.Setenv("HOME", tmp)
			t.Setenv("XDG_CACHE_HOME", tmp)
			t.Setenv("LOCALAPPDATA", tmp)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				_, _ = w.Write([]byte(tc.bodies[n-1]))
			}))
			defer server.Close()
			var logs []string
			asset := ghAsset{Name: "CARE-Clinic-1.2.3-setup.exe", URL: server.URL}
			path, err := downloadVerified(asset, want, "1.2.3", func(line string) { logs = append(logs, line) }, nil)
			if got := requests.Load(); got != tc.requests {
				t.Fatalf("made %d download attempts, want %d", got, tc.requests)
			}
			if tc.installed {
				data, readErr := os.ReadFile(path)
				if err != nil || readErr != nil || string(data) != string(good) {
					t.Fatalf("verified download missing: %v, %v", err, readErr)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "didn't download properly") ||
				!strings.Contains(err.Error(), "choose Update again") {
				t.Fatalf("damaged download was not rejected clearly: %v", err)
			}
			if !strings.Contains(strings.Join(logs, "\n"), "Trying once more") {
				t.Fatalf("retry was not logged: %q", logs)
			}
			cache, _ := os.UserCacheDir()
			if left, _ := os.ReadDir(filepath.Join(cache, "CARE Clinic", "updates")); len(left) != 0 {
				t.Fatalf("damaged download was left behind: %v", left)
			}
		})
	}
}

func TestDownloadRejectsUnsafeAssetAndChecksum(t *testing.T) {
	sum := strings.Repeat("a", 64)
	for _, name := range []string{"../installer.exe", `..\installer.exe`, "", "."} {
		if _, err := downloadVerified(ghAsset{Name: name}, sum, "1.2.3", nil, nil); err == nil {
			t.Errorf("unsafe filename %q accepted", name)
		}
	}
	if _, err := downloadVerified(ghAsset{Name: "installer.exe"}, "invalid", "1.2.3", nil, nil); err == nil {
		t.Fatal("invalid checksum accepted")
	}
}

func TestSwapScriptReplacesBundleOrKeepsTheOldOne(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the in-place app swap is macOS only")
	}
	bundle := func(dir, marker string) string {
		app := filepath.Join(dir, "CARE Clinic.app")
		if err := os.MkdirAll(filepath.Join(app, "Contents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(app, "Contents", "marker"), []byte(marker), 0o644); err != nil {
			t.Fatal(err)
		}
		return app
	}
	marker := func(app string) string {
		data, _ := os.ReadFile(filepath.Join(app, "Contents", "marker"))
		return string(data)
	}
	tmp := t.TempDir()
	apps := filepath.Join(tmp, "Applications")
	target := bundle(apps, "old")
	staged := bundle(filepath.Join(tmp, "stage"), "new")

	if err := elevate.Run(swapScript(target, filepath.Join(tmp, "missing.app")), false); err == nil {
		t.Fatal("a swap from a missing bundle reported success")
	}
	if got := marker(target); got != "old" {
		t.Fatalf("a failed swap left %q installed, want the old bundle back", got)
	}

	brokenRename := strings.Replace(swapScript(target, staged),
		`/bin/mv "$slot/new.app"`, `/usr/bin/false`, 1)
	if err := elevate.Run(brokenRename, false); err == nil {
		t.Fatal("failed final rename reported success")
	}
	if got := marker(target); got != "old" {
		t.Fatalf("failed final rename did not restore old bundle: %q", got)
	}
	// Failed transactions retain their private directory for diagnosis.
	for _, entry := range []string{".care-update-" + filepath.Base(tmp)} {
		if err := os.RemoveAll(filepath.Join(apps, entry)); err != nil {
			t.Fatal(err)
		}
	}

	if err := elevate.Run(swapScript(target, staged), false); err != nil {
		t.Fatal(err)
	}
	if got := marker(target); got != "new" {
		t.Fatalf("installed bundle is %q, want new", got)
	}
	if left, _ := os.ReadDir(apps); len(left) != 1 {
		t.Fatalf("the swap left extra entries behind: %v", left)
	}
}
