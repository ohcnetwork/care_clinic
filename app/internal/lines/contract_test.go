package lines

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

// The CI scripts under images/scripts produce what this package reads; these
// tests keep the two sides of that contract in step.

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func node(t *testing.T, dir string, args ...string) string {
	t.Helper()
	if !proc.Exists("node") {
		t.Skip("Node.js is not installed")
	}
	cmd := proc.Command("node", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestRenderedManifestParses(t *testing.T) {
	root := repoRoot(t)
	tmp := t.TempDir()
	frontend := filepath.Join(tmp, "frontend.json")
	node(t, root, "images/scripts/frontend-env.mjs", "--out", filepath.Join(tmp, "clinic.env"), "--json", frontend)
	data, err := os.ReadFile(frontend)
	if err != nil {
		t.Fatal(err)
	}
	var fe struct {
		RuntimeKeys []string          `json:"runtime_keys"`
		BuildEnv    map[string]string `json:"build_env"`
	}
	if err := json.Unmarshal(data, &fe); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(root, "images", "lines.source.json"))
	if err != nil {
		t.Fatal(err)
	}
	var src struct {
		Lines []struct {
			ID string `json:"id"`
		} `json:"lines"`
	}
	if err := json.Unmarshal(source, &src); err != nil {
		t.Fatal(err)
	}
	builds := filepath.Join(tmp, "builds")
	if err := os.MkdirAll(builds, 0o700); err != nil {
		t.Fatal(err)
	}
	digest := "@sha256:" + strings.Repeat("a", 64)
	for _, l := range src.Lines {
		build := map[string]any{
			"backend": map[string]any{"commit": strings.Repeat("b", 40), "image": "ghcr.io/x/backend" + digest, "builder": "ghcr.io/x/backend-builder" + digest},
			"frontend": map[string]any{"commit": strings.Repeat("f", 40), "image": "ghcr.io/x/frontend" + digest, "builder": "ghcr.io/x/frontend-builder" + digest,
				"runtime_keys": fe.RuntimeKeys, "build_env": fe.BuildEnv},
		}
		data, _ := json.Marshal(build)
		if err := os.WriteFile(filepath.Join(builds, l.ID+".json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(tmp, "lines.json")
	node(t, root, "images/scripts/render-lines.mjs", "--source", "images/lines.source.json", "--builds", builds, "--out", out)
	rendered, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Parse(rendered)
	if err != nil {
		t.Fatalf("rendered manifest does not parse: %v\n%s", err, rendered)
	}
	if len(m.Lines) != len(src.Lines) {
		t.Fatalf("rendered %d lines, source has %d", len(m.Lines), len(src.Lines))
	}
	for _, l := range m.Lines {
		if !slices.Contains(l.Frontend.RuntimeKeys, "REACT_CARE_API_URL") {
			t.Fatalf("line %s cannot follow the clinic address without a rebuild", l.ID)
		}
	}
}

func TestFrontendBuildMatchesNewClinicSettings(t *testing.T) {
	root := repoRoot(t)
	tmp := t.TempDir()
	env, frontend := filepath.Join(tmp, "clinic.env"), filepath.Join(tmp, "frontend.json")
	node(t, root, "images/scripts/frontend-env.mjs", "--out", env, "--json", frontend)
	compiled, err := os.ReadFile(env)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := os.ReadFile(filepath.Join(root, "images", "frontend", "runtime-keys.json"))
	if err != nil {
		t.Fatal(err)
	}
	var runtime map[string]string
	if err := json.Unmarshal(keys, &runtime); err != nil {
		t.Fatal(err)
	}
	for k := range runtime {
		if !strings.Contains(string(compiled), k+"='https://care-clinic.invalid/__CARE_CLINIC_"+k+"__'\n") {
			t.Fatalf("%s is not compiled as a placeholder:\n%s", k, compiled)
		}
	}
	// A new clinic's compile-time settings must equal the image's, or every
	// install would rebuild the frontend locally.
	clinic, err := os.ReadFile(filepath.Join(root, "deployments", "frontend.env"))
	if err != nil {
		t.Fatal(err)
	}
	var fe struct {
		BuildEnv map[string]string `json:"build_env"`
	}
	data, _ := os.ReadFile(frontend)
	if err := json.Unmarshal(data, &fe); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(clinic), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.HasPrefix(k, "#") || v == "" {
			continue
		}
		if _, isRuntime := runtime[k]; isRuntime {
			continue
		}
		if fe.BuildEnv[k] != v {
			t.Fatalf("%s=%s in deployments/frontend.env is not what the image is compiled with (%q)", k, v, fe.BuildEnv[k])
		}
	}
}
