package compose

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/lines"
	"github.com/ohcnetwork/care_desktop/app/internal/release"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

func writeBuildFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func digestRef(name string, c byte) string {
	return "ghcr.io/ohcnetwork/care-clinic-" + name + "@sha256:" + strings.Repeat(string(c), 64)
}

func lineFixture(id string, c byte) lines.Line {
	return lines.Line{
		ID: id,
		Backend: lines.Image{
			Commit:  strings.Repeat(string(c), 40),
			Image:   digestRef("backend", c),
			Builder: digestRef("backend-builder", c),
		},
		Frontend: lines.Image{
			Commit:      strings.Repeat(string(c), 40),
			Image:       digestRef("frontend", c),
			Builder:     digestRef("frontend-builder", c),
			RuntimeKeys: []string{"REACT_CARE_API_URL", "REACT_APP_TITLE"},
			BuildEnv:    map[string]string{"REACT_MFE_REGISTERED_COMPONENTS": "UserDashboard"},
		},
	}
}

func publish(t *testing.T, b *Builder, m lines.Manifest) {
	t.Helper()
	m.Schema = lines.Schema
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	writeBuildFile(t, filepath.Dir(b.set.LinesURL), filepath.Base(b.set.LinesURL), string(data))
}

func builderFixture(t *testing.T) *Builder {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"backend.env":       "ADDITIONAL_PLUGS='[{\"name\":\"one\",\"package_name\":\"pkg_one\"}]'\nPOSTGRES_HOST=db\n",
		"frontend.env":      "REACT_CARE_API_URL=https://care.local\nREACT_MFE_REGISTERED_COMPONENTS=UserDashboard\n",
		"backup.Dockerfile": "ARG POSTGRES_IMAGE\nFROM ${POSTGRES_IMAGE}\nRUN apk add --no-cache openssl\n",
		"caddy.Dockerfile":  "ARG CADDY_IMAGE\nFROM ${CADDY_IMAGE}\nARG CORAZA_VERSION\n",
	} {
		writeBuildFile(t, dir, name, content)
	}
	b := NewBuilder(proc.Runner{Dir: dir}, dir, &release.Pins{
		AppVersion:    "0.1.0-dev",
		LinesURL:      filepath.Join(t.TempDir(), "lines.json"),
		DefaultLine:   "stable",
		BackendImage:  "care:clinic",
		FrontendImage: "care_fe:clinic",
		BackupImage:   "care-backup:clinic",
		CaddyWafImage: "care-caddy:clinic",
		PostgresImage: "postgres:17-alpine",
		CaddyImage:    "caddy:2",
		CorazaVersion: "v2.6.1",
	}, nil)
	publish(t, b, lines.Manifest{Lines: []lines.Line{lineFixture("stable", 'a')}})
	return b
}

func key(t *testing.T, plan func() (plan, error)) string {
	t.Helper()
	p, err := plan()
	if err != nil {
		t.Fatal(err)
	}
	return p.key
}

func imageKeys(t *testing.T, b *Builder) [4]string {
	t.Helper()
	backup, err := b.backupBuiltFrom()
	if err != nil {
		t.Fatal(err)
	}
	caddy, err := b.caddyBuiltFrom()
	if err != nil {
		t.Fatal(err)
	}
	be := key(t, b.backendPlan)
	fe := key(t, b.frontendPlan)
	return [4]string{be, fe, backup, caddy}
}

func TestImageKeysTrackBuildInputs(t *testing.T) {
	tests := []struct {
		name    string
		changed [4]bool
		mutate  func(*testing.T, *Builder)
	}{
		{"app version", [4]bool{}, func(t *testing.T, b *Builder) { b.set.AppVersion = "0.2.0-dev" }},
		{"backend release", [4]bool{true}, func(t *testing.T, b *Builder) {
			l := lineFixture("stable", 'a')
			l.Backend = lineFixture("x", 'b').Backend
			publish(t, b, lines.Manifest{Lines: []lines.Line{l}})
		}},
		{"frontend release", [4]bool{false, true}, func(t *testing.T, b *Builder) {
			l := lineFixture("stable", 'a')
			l.Frontend = lineFixture("x", 'b').Frontend
			publish(t, b, lines.Manifest{Lines: []lines.Line{l}})
		}},
		{"plugins", [4]bool{true}, func(t *testing.T, b *Builder) {
			writeBuildFile(t, b.dir, "backend.env", "ADDITIONAL_PLUGS='[{\"name\":\"two\",\"package_name\":\"pkg_two\"}]'\n")
		}},
		{"frontend runtime setting", [4]bool{}, func(t *testing.T, b *Builder) {
			writeBuildFile(t, b.dir, "frontend.env", "REACT_CARE_API_URL=https://other.local\nREACT_APP_TITLE=Clinic\n")
		}},
		{"frontend build setting", [4]bool{false, true}, func(t *testing.T, b *Builder) {
			writeBuildFile(t, b.dir, "frontend.env", "REACT_MFE_REGISTERED_COMPONENTS=Other\n")
		}},
		{"blank build setting", [4]bool{}, func(t *testing.T, b *Builder) {
			writeBuildFile(t, b.dir, "frontend.env", "REACT_MFE_REGISTERED_COMPONENTS=\n")
		}},
		{"postgres", [4]bool{false, false, true}, func(t *testing.T, b *Builder) { b.set.PostgresImage = "postgres:18-alpine" }},
		{"backup Dockerfile", [4]bool{false, false, true}, func(t *testing.T, b *Builder) {
			writeBuildFile(t, b.dir, "backup.Dockerfile", "ARG POSTGRES_IMAGE\nFROM ${POSTGRES_IMAGE}\nRUN apk add openssl tar\n")
		}},
		{"prebuilt backup", [4]bool{false, false, true}, func(t *testing.T, b *Builder) { b.set.BackupPrebuilt = digestRef("backup", 'c') }},
		{"caddy", [4]bool{false, false, false, true}, func(t *testing.T, b *Builder) { b.set.CaddyImage = "caddy:2.11" }},
		{"coraza", [4]bool{false, false, false, true}, func(t *testing.T, b *Builder) { b.set.CorazaVersion = "v2.7.0" }},
		{"caddy Dockerfile", [4]bool{false, false, false, true}, func(t *testing.T, b *Builder) {
			writeBuildFile(t, b.dir, "caddy.Dockerfile", "ARG CADDY_IMAGE\nFROM ${CADDY_IMAGE}\nARG CORAZA_VERSION\nRUN caddy version\n")
		}},
		{"prebuilt caddy", [4]bool{false, false, false, true}, func(t *testing.T, b *Builder) { b.set.CaddyWafPrebuilt = digestRef("caddy", 'c') }},
		{"runtime files", [4]bool{}, func(t *testing.T, b *Builder) {
			writeBuildFile(t, b.dir, "backend.env", "POSTGRES_HOST=other-db\nBUCKET_KEY=custom-user\nFILE_UPLOAD_BUCKET=records\nADDITIONAL_PLUGS='[{\"name\":\"one\",\"package_name\":\"pkg_one\"}]'\n")
			writeBuildFile(t, b.dir, "Caddyfile", "other.local:443 {}\n")
			writeBuildFile(t, b.dir, "scripts/backup.sh", "exit 0\n")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := builderFixture(t)
			before := imageKeys(t, b)
			tt.mutate(t, b)
			after := imageKeys(t, b)
			for i, wantChanged := range tt.changed {
				if gotChanged := before[i] != after[i]; gotChanged != wantChanged {
					t.Errorf("image %d changed = %v, want %v (%s -> %s)", i, gotChanged, wantChanged, before[i], after[i])
				}
			}
		})
	}
}

func TestPlainImagesUseThePublishedDigest(t *testing.T) {
	b := builderFixture(t)
	writeBuildFile(t, b.dir, "backend.env", "POSTGRES_HOST=db\n")
	if got := imageKeys(t, b); got[0] != digestRef("backend", 'a') || got[1] != digestRef("frontend", 'a') {
		t.Fatalf("keys = %v", got)
	}
}

func TestUnpublishedLineIsAnError(t *testing.T) {
	b := builderFixture(t)
	b.set.DefaultLine = "missing"
	if _, err := b.backendPlan(); err == nil {
		t.Fatal("an unpublished line produced an image")
	}
}

func TestLineRequiringANewerAppIsRefused(t *testing.T) {
	b := builderFixture(t)
	l := lineFixture("stable", 'a')
	l.MinAppVersion = "9.0.0"
	publish(t, b, lines.Manifest{Lines: []lines.Line{l}})
	if _, err := b.frontendPlan(); err == nil || !strings.Contains(err.Error(), "9.0.0") {
		t.Fatalf("line for a newer app was accepted: %v", err)
	}
}

func TestBuildSettingsMergeOnlyCompileTimeValues(t *testing.T) {
	rel := lineFixture("stable", 'a').Frontend
	merged, changed := BuildSettings(rel, map[string]string{
		"REACT_CARE_API_URL":              "https://x",
		"REACT_MFE_REGISTERED_COMPONENTS": "UserDashboard",
		"REACT_CUSTOM_DESCRIPTION":        "",
	})
	if changed || len(merged) != 1 {
		t.Fatalf("runtime or blank values forced a rebuild: %v %v", merged, changed)
	}
	merged, changed = BuildSettings(rel, map[string]string{"REACT_CUSTOM_DESCRIPTION": "Ours"})
	if !changed || merged["REACT_CUSTOM_DESCRIPTION"] != "Ours" || merged["REACT_MFE_REGISTERED_COMPONENTS"] != "UserDashboard" {
		t.Fatalf("merge = %v %v", merged, changed)
	}
	if rel.BuildEnv["REACT_CUSTOM_DESCRIPTION"] != "" {
		t.Fatal("merge mutated the release")
	}
}

func TestRenderEnvIsStableAndQuoted(t *testing.T) {
	got := renderEnv(map[string]string{"B": "it's", "A": "x y", "C": `a"b'c`})
	want := "A='x y'\nB=\"it's\"\nC=`a\"b'c`\n"
	if got != want {
		t.Fatalf("renderEnv = %q, want %q", got, want)
	}
}

func dockerFixture(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	dir := t.TempDir()
	trace := filepath.Join(dir, "calls")
	t.Setenv("CARE_BUILD_TRACE", trace)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := `#!/bin/sh
printf '%s\n' "$@" >> "$CARE_BUILD_TRACE"
printf '%s\n' '--' >> "$CARE_BUILD_TRACE"
case "$1 $2" in
  'image ls')
    [ "$CARE_FAIL_LIST" != yes ] || exit 21
    [ "$CARE_IMAGE_EXISTS" != yes ] || printf 'image-id\n'
    ;;
  'image inspect')
    if [ "$3" = --format ]; then
      [ "$CARE_REFS_LOCAL" = yes ] || exit 1
      printf 'sha256:local\n'
      exit 0
    fi
    [ "$CARE_FAIL_INSPECT" != yes ] || exit 22
    printf '%s\n' "$CARE_IMAGE_KEY"
    ;;
  'image prune'|'image tag') ;;
  pull*) [ "$CARE_FAIL_PULL" != yes ] || exit 24 ;;
  build*)
    for arg do context=$arg; done
    [ -d "$context" ] || exit 23
    [ ! -f "$context/clinic.env" ] || cp "$context/clinic.env" "$CARE_BUILD_TRACE.env"
    ;;
  *) exit 99 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return trace
}

func readTrace(t *testing.T, trace string) string {
	t.Helper()
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestEnsureDistinguishesMissingImagesFromInspectionFailures(t *testing.T) {
	for _, tt := range []struct {
		name    string
		exists  string
		key     string
		failure string
		builds  int
	}{
		{"missing", "", "", "", 1},
		{"current", "yes", "wanted", "", 0},
		{"stale", "yes", "older", "", 1},
		{"unrecorded", "yes", "<no value>", "", 1},
		{"list failure", "yes", "", "CARE_FAIL_LIST", 0},
		{"inspect failure", "yes", "", "CARE_FAIL_INSPECT", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dockerFixture(t)
			t.Setenv("CARE_IMAGE_EXISTS", tt.exists)
			t.Setenv("CARE_IMAGE_KEY", tt.key)
			if tt.failure != "" {
				t.Setenv(tt.failure, "yes")
			}
			builds := 0
			err := builderFixture(t).ensure("care:clinic", "wanted", "backend", func() error {
				builds++
				return nil
			})
			if (err != nil) != (tt.failure != "") {
				t.Errorf("unexpected inspection result: %v", err)
			}
			if builds != tt.builds {
				t.Errorf("built %d images, want %d", builds, tt.builds)
			}
		})
	}
}

func TestBuildAndEnsurePropagateInputReadErrors(t *testing.T) {
	for _, name := range []string{"backend.env", "frontend.env", "backup.Dockerfile", "caddy.Dockerfile"} {
		for _, failure := range []string{"missing", "directory"} {
			t.Run(name+"/"+failure, func(t *testing.T) {
				trace := dockerFixture(t)
				b := builderFixture(t)
				path := filepath.Join(b.dir, name)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if failure == "directory" {
					if err := os.Mkdir(path, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				operations := map[string][]func() error{
					"backend.env":       {b.BuildBackend, b.EnsureBackendImage},
					"frontend.env":      {b.BuildFrontend, b.EnsureFrontendImage},
					"backup.Dockerfile": {b.BuildBackup, b.EnsureBackupImage},
					"caddy.Dockerfile":  {b.BuildCaddy, b.EnsureCaddyImage},
				}
				for _, operation := range operations[name] {
					if err := operation(); err == nil {
						t.Fatal("unreadable build input was ignored")
					}
				}
				if _, err := os.Stat(trace); !os.IsNotExist(err) {
					t.Fatalf("Docker was called before inputs were readable: %v", err)
				}
			})
		}
	}
	t.Run("malformed plugins", func(t *testing.T) {
		trace := dockerFixture(t)
		b := builderFixture(t)
		writeBuildFile(t, b.dir, "backend.env", "ADDITIONAL_PLUGS='unterminated\n")
		for _, operation := range []func() error{b.BuildBackend, b.EnsureBackendImage} {
			if err := operation(); err == nil {
				t.Fatal("malformed build inputs were accepted")
			}
		}
		if _, err := os.Stat(trace); !os.IsNotExist(err) {
			t.Fatalf("Docker was called with malformed inputs: %v", err)
		}
	})
}

func TestBuildLabelsMatchConsumedInputs(t *testing.T) {
	trace := dockerFixture(t)
	b := builderFixture(t)
	writeBuildFile(t, b.dir, "frontend.env", "REACT_CARE_API_URL=https://care.local\nREACT_CUSTOM_DESCRIPTION=Our clinic\n")
	keys := imageKeys(t, b)
	for _, build := range []func() error{b.BuildBackend, b.BuildFrontend, b.BuildBackup, b.BuildCaddy} {
		if err := build(); err != nil {
			t.Fatal(err)
		}
	}
	data := readTrace(t, trace)
	for _, key := range keys {
		if !strings.Contains(data, builtFromLabel+"="+key+"\n") {
			t.Errorf("build did not label its inputs: %s", key)
		}
	}
	rel := lineFixture("stable", 'a')
	for _, want := range []string{
		"pull\n" + rel.Backend.Builder + "\n",
		"BACKEND_BUILDER=" + rel.Backend.Builder + "\n",
		"BACKEND_IMAGE=" + rel.Backend.Image + "\n",
		`ADDITIONAL_PLUGS=[{"name":"one","package_name":"pkg_one"}]` + "\n",
		"FRONTEND_BUILDER=" + rel.Frontend.Builder + "\n",
		filepath.Join(b.dir, backendPluginsDockerfile) + "\n",
		filepath.Join(b.dir, frontendRebuildDockerfile) + "\n",
	} {
		if !strings.Contains(data, want) {
			t.Errorf("docker was not called with %q", want)
		}
	}
	env := readTrace(t, trace+".env")
	for _, want := range []string{
		"REACT_CARE_API_URL='https://care-clinic.invalid/__CARE_CLINIC_REACT_CARE_API_URL__'\n",
		"REACT_APP_TITLE='https://care-clinic.invalid/__CARE_CLINIC_REACT_APP_TITLE__'\n",
		"REACT_CUSTOM_DESCRIPTION='Our clinic'\n",
		"REACT_MFE_REGISTERED_COMPONENTS='UserDashboard'\n",
	} {
		if !strings.Contains(env, want) {
			t.Errorf("frontend rebuild env lacks %q:\n%s", want, env)
		}
	}
	contexts, err := filepath.Glob(filepath.Join(b.dir, ".care-buildctx-*"))
	if err != nil || len(contexts) != 0 {
		t.Fatalf("empty build contexts were not cleaned up: %v, %v", contexts, err)
	}
}

func TestLocalImagesAreNotDownloadedAgain(t *testing.T) {
	trace := dockerFixture(t)
	t.Setenv("CARE_REFS_LOCAL", "yes")
	b := builderFixture(t)
	writeBuildFile(t, b.dir, "backend.env", "POSTGRES_HOST=db\n")
	if err := b.BuildBackend(); err != nil {
		t.Fatal(err)
	}
	if data := readTrace(t, trace); strings.Contains(data, "pull\n") {
		t.Fatal("an image already on this computer was downloaded again")
	}
}

func TestFailedDownloadIsReported(t *testing.T) {
	dockerFixture(t)
	t.Setenv("CARE_FAIL_PULL", "yes")
	b := builderFixture(t)
	if err := b.EnsureFrontendImage(); err == nil || !strings.Contains(err.Error(), "network") {
		t.Fatalf("download failure = %v", err)
	}
	if ReadLock(b.dir).Frontend.Current.Image != "" {
		t.Fatal("a failed download was recorded as installed")
	}
}
