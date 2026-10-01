package prereq

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/release"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

func TestDownloadPreviewOnlyRequestsHeaders(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/installer", http.StatusFound)
			return
		}
		w.Header().Set("Content-Length", "734003200")
	}))
	defer server.Close()
	info, err := inspectDownload(Download{Name: "Rancher.dmg", URL: server.URL + "/redirect"})
	if err != nil || info.Name != "Rancher.dmg" || info.Size != 734003200 {
		t.Fatalf("incorrect preview: %+v, %v", info, err)
	}
	if strings.Join(methods, ",") != "HEAD,HEAD" {
		t.Fatalf("preview requested installer content: %v", methods)
	}
}

func TestDownloadPreviewRejectsUnavailableSize(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusMethodNotAllowed} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer server.Close()
			if _, err := inspectDownload(Download{Name: "installer", URL: server.URL}); err == nil {
				t.Fatal("missing size or failed request accepted")
			}
		})
	}
}

func TestDownloadReportsBytesAndVerification(t *testing.T) {
	body := strings.Repeat("synthetic installer", 8192)
	hash := sha256.Sum256([]byte(body))
	for _, test := range []struct {
		name  string
		known bool
		valid bool
	}{
		{"known size", true, true},
		{"unknown size", false, true},
		{"checksum failure", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.known {
					w.Header().Set("Content-Length", fmt.Sprint(len(body)))
				} else {
					w.(http.Flusher).Flush()
				}
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			var events []DownloadProgress
			pr := &Provisioner{Progress: func(p DownloadProgress) { events = append(events, p) }}
			sum := hex.EncodeToString(hash[:])
			if !test.valid {
				sum = strings.Repeat("0", 64)
			}
			path, err := pr.download(Download{Name: "fixture-installer", URL: server.URL, SHA256: sum})
			if test.valid {
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := os.Remove(path); err != nil {
						t.Errorf("remove downloaded fixture: %v", err)
					}
				})
				data, err := os.ReadFile(path)
				if err != nil || string(data) != body {
					t.Fatal("download contents changed")
				}
			} else if err == nil || path != "" {
				t.Fatal("checksum mismatch did not fail")
			}
			if len(events) < 4 || events[0].Phase != "connecting" || events[1].Phase != "downloading" {
				t.Fatalf("missing download lifecycle: %+v", events)
			}
			last := events[len(events)-1]
			wantPhase := "complete"
			if !test.valid {
				wantPhase = "failed"
			}
			total := int64(0)
			if test.known {
				total = int64(len(body))
			}
			if last.Phase != wantPhase || last.Done != int64(len(body)) || last.Total != total {
				t.Fatalf("incorrect terminal progress: %+v", last)
			}
			var previous int64
			sawBytes := false
			for _, event := range events {
				if event.Name != "fixture-installer" || event.Done < previous {
					t.Fatalf("invalid progress: %+v", event)
				}
				if event.Phase == "complete" && !test.valid {
					t.Fatal("verification failure reported completion")
				}
				previous = event.Done
				if event.Phase == "downloading" && event.Done > 0 {
					sawBytes = true
				}
			}
			if !sawBytes {
				t.Fatal("missing byte progress while downloading")
			}
		})
	}
}

func deploymentPins(t *testing.T) *release.Pins {
	t.Helper()
	data, err := os.ReadFile("../../../deployments/.env")
	if err != nil {
		t.Fatal(err)
	}
	pins, err := release.Load(data)
	if err != nil {
		t.Fatal(err)
	}
	return pins
}

func pinnedDownloads(t *testing.T, p *release.Pins) []Download {
	t.Helper()
	var all []Download
	for _, target := range [][2]string{{"darwin", "arm64"}, {"darwin", "amd64"}, {"windows", "amd64"}} {
		d, err := rancherDownload(p, target[0], target[1])
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, d)
	}
	all = append(all, gitWindowsDownload(p))
	for _, goarch := range []string{"amd64", "arm64"} {
		engine, compose, err := dockerLinuxDownloads(p, goarch)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, engine, compose)
	}
	return all
}

func TestRancherDownloadMatchesPlatform(t *testing.T) {
	p := &release.Pins{
		RancherVersion:        "1.24.0",
		RancherMacArm64SHA256: "arm", RancherMacX8664SHA256: "intel", RancherWindowsSHA256: "windows",
	}
	base := "https://github.com/rancher-sandbox/rancher-desktop/releases/download/v1.24.0/"
	for _, tc := range []struct{ goos, goarch, name, sum string }{
		{"darwin", "arm64", "Rancher.Desktop-1.24.0.aarch64.dmg", "arm"},
		{"darwin", "amd64", "Rancher.Desktop-1.24.0.x86_64.dmg", "intel"},
		{"windows", "amd64", "Rancher.Desktop.Setup.1.24.0.msi", "windows"},
	} {
		d, err := rancherDownload(p, tc.goos, tc.goarch)
		if err != nil || d != (Download{Name: tc.name, URL: base + tc.name, SHA256: tc.sum}) {
			t.Errorf("%s/%s: got %+v, %v", tc.goos, tc.goarch, d, err)
		}
	}
	for _, target := range [][2]string{{"windows", "arm64"}, {"linux", "amd64"}} {
		if _, err := rancherDownload(p, target[0], target[1]); err == nil {
			t.Errorf("%s/%s: expected no Rancher Desktop download", target[0], target[1])
		}
	}
}

func TestDockerLinuxDownloadsMatchArchitecture(t *testing.T) {
	p := &release.Pins{
		DockerLinuxVersion: "29.8.1", DockerLinuxX8664SHA256: "engine-intel", DockerLinuxAarch64SHA256: "engine-arm",
		ComposeLinuxVersion: "5.5.1", ComposeLinuxX8664SHA256: "compose-intel", ComposeLinuxAarch64SHA256: "compose-arm",
	}
	for _, tc := range []struct{ goarch, arch, engineSum, composeSum string }{
		{"amd64", "x86_64", "engine-intel", "compose-intel"},
		{"arm64", "aarch64", "engine-arm", "compose-arm"},
	} {
		engine, compose, err := dockerLinuxDownloads(p, tc.goarch)
		if err != nil {
			t.Fatal(err)
		}
		wantEngine := Download{
			Name:   "docker-29.8.1.tgz",
			URL:    "https://download.docker.com/linux/static/stable/" + tc.arch + "/docker-29.8.1.tgz",
			SHA256: tc.engineSum,
		}
		wantCompose := Download{
			Name:   "docker-compose-linux-" + tc.arch,
			URL:    "https://github.com/docker/compose/releases/download/v5.5.1/docker-compose-linux-" + tc.arch,
			SHA256: tc.composeSum,
		}
		if engine != wantEngine || compose != wantCompose {
			t.Errorf("%s: got %+v and %+v", tc.goarch, engine, compose)
		}
	}
	if _, _, err := dockerLinuxDownloads(p, "riscv64"); err == nil {
		t.Error("riscv64: expected no Docker download")
	}
}

func TestGitWindowsDownloadName(t *testing.T) {
	for version, name := range map[string]string{
		"2.55.0.windows.1": "Git-2.55.0-64-bit.exe",
		"2.55.0.windows.5": "Git-2.55.0.5-64-bit.exe",
	} {
		d := gitWindowsDownload(&release.Pins{GitWindowsVersion: version, GitWindowsSHA256: "sum"})
		want := Download{
			Name:   name,
			URL:    "https://github.com/git-for-windows/git/releases/download/v" + version + "/" + name,
			SHA256: "sum",
		}
		if d != want {
			t.Errorf("%s: got %+v, want %+v", version, d, want)
		}
	}
}

func TestReleaseWorkflowVerifiesTheDownloadsTheAppUses(t *testing.T) {
	if !proc.Exists("node") {
		t.Skip("Node.js is not installed")
	}
	expected, err := json.Marshal(pinnedDownloads(t, deploymentPins(t)))
	if err != nil {
		t.Fatal(err)
	}
	script := `
const fs = require("fs");
const vm = require("vm");
const crypto = require("crypto");
const assert = require("assert/strict");
const workflow = fs.readFileSync(".github/workflows/release.yml", "utf8");
const match = workflow.match(/          node <<'PREREQS'\n([\s\S]*?)\n          PREREQS/);
if (!match) throw new Error("Prerequisite verifier is missing");
const verifier = match[1].replace(/^          /gm, "");
const downloads = JSON.parse(process.env.CARE_DOWNLOADS);
const pins = fs.readFileSync("deployments/.env", "utf8");
const body = url => "installer served from " + url;
const hash = value => crypto.createHash("sha256").update(value).digest("hex");
let served = pins;
for (const d of downloads) served = served.replace(d.SHA256, hash(body(d.URL)));
const run = async (env, missing) => {
  const fetched = [];
  const logs = [];
  const proc = {env: {}, exitCode: undefined};
  await vm.runInNewContext(verifier, {
    process: proc,
    console: {log: line => logs.push(line)},
    fetch: async url => {
      fetched.push(url);
      if (url === missing) return {ok: false, status: 404};
      return {ok: true, status: 200, body: (async function* () { yield Buffer.from(body(url)); })()};
    },
    require: name => {
      if (name === "crypto") return crypto;
      assert.equal(name, "fs");
      return {readFileSync: path => { assert.equal(path, "deployments/.env"); return env; }};
    },
  });
  return {fetched: fetched.sort(), logs, failed: proc.exitCode === 1};
};
(async () => {
  const urls = downloads.map(d => d.URL).sort();
  const ok = await run(served);
  assert.deepEqual(ok.fetched, urls);
  assert.equal(ok.failed, false, ok.logs.join("\n"));
  const first = downloads[0];
  const tampered = await run(served.replace(hash(body(first.URL)), "0".repeat(64)));
  assert.equal(tampered.failed, true);
  assert.ok(tampered.logs.some(line => line.startsWith("::error::") && line.includes(first.URL)));
  const gone = await run(served, downloads[3].URL);
  assert.equal(gone.failed, true);
  assert.ok(gone.logs.some(line => line.includes("HTTP 404")));
  let threw = false;
  try { await run(served.replace(/^GIT_WINDOWS_VERSION=.*$/m, "GIT_WINDOWS_VERSION=latest")); } catch { threw = true; }
  assert.ok(threw, "an unpinned Git version was accepted");
})().catch(error => { console.error(error); process.exit(1); });
`
	cmd := proc.Command("node", "-e", script)
	cmd.Dir = "../../.."
	cmd.Env = append(os.Environ(), "CARE_DOWNLOADS="+string(expected))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("prerequisite verifier failed: %v\n%s", err, output)
	}
}
