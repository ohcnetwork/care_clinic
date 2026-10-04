package residue

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"text/template"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

func TestUnavailableDockerIsNotClean(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX command fixtures")
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	if err := os.WriteFile(filepath.Join(root, "docker"), []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	run := proc.Runner{Env: os.Environ()}
	report, err := scan(Options{
		Runner: run, Project: "care-clinic", InstallDir: filepath.Join(root, "install"),
	}, noSystemTraces)
	if err == nil || report.Clean {
		t.Fatalf("unavailable Docker was called clean: %+v, %v", report, err)
	}
	if _, err := InstallDirFrom(run, "care-clinic", filepath.Join(root, "missing")); err == nil {
		t.Fatal("failed installation discovery was ignored")
	}
}

func TestMissingDockerIsNotDockerResidue(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX command fixtures")
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("PATH", filepath.Join(root, "empty"))
	run := proc.Runner{Env: os.Environ()}
	report, err := scan(Options{
		Runner: run, Project: "care-clinic", InstallDir: filepath.Join(root, "install"),
		Images: []string{"care:clinic"},
	}, noSystemTraces)
	if err != nil || !report.Clean || len(report.Traces) != 0 {
		t.Fatalf("a computer without Docker is not dirty: %+v, %v", report, err)
	}
	data, err := json.Marshal(report)
	if err != nil || string(data) != `{"clean":true,"traces":[]}` {
		t.Fatalf("empty scan must serialize traces as an array: %s, %v", data, err)
	}
	dir, err := InstallDirFrom(run, "care-clinic", filepath.Join(root, "missing"))
	if err != nil || dir != filepath.Join(root, "missing") {
		t.Fatalf("installation discovery needed Docker: %q, %v", dir, err)
	}
	// Files and native traces are still found without a container engine.
	if err := os.MkdirAll(filepath.Join(root, "install"), 0o700); err != nil {
		t.Fatal(err)
	}
	report, err = scan(Options{
		Runner: run, Project: "care-clinic", InstallDir: filepath.Join(root, "install"),
	}, func(proc.Runner) ([]Trace, error) { return []Trace{{ID: "hosts"}}, nil })
	if err != nil || report.Clean || len(report.Traces) != 2 {
		t.Fatalf("file and system traces were lost without Docker: %+v, %v", report, err)
	}
}

func TestStoppedDockerIsDistinguishedFromAnAbsentOne(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX command fixtures")
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	if err := os.WriteFile(filepath.Join(root, "docker"), []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	report, err := scan(Options{
		Runner: proc.Runner{Env: os.Environ()}, Project: "care-clinic",
		InstallDir: filepath.Join(root, "install"),
	}, noSystemTraces)
	if err == nil || report.Clean || !strings.Contains(err.Error(), "start Docker and try again") {
		t.Fatalf("an installed but stopped Docker was treated as absent: %+v, %v", report, err)
	}
}

type dockerPsRow struct{ labels map[string]string }

func (r dockerPsRow) Labels() string {
	var out []string
	for k, v := range r.labels {
		out = append(out, k+"="+v)
	}
	return strings.Join(out, ",")
}

func (r dockerPsRow) Label(name string) string { return r.labels[name] }

func TestWorkingDirFormatMatchesDockerPsContext(t *testing.T) {
	tmpl, err := template.New("").Parse(workingDirFormat)
	if err != nil {
		t.Fatal(err)
	}
	row := dockerPsRow{labels: map[string]string{
		"com.docker.compose.project":             "care-clinic",
		"com.docker.compose.project.working_dir": "/clinic/install",
	}}
	var out strings.Builder
	if err := tmpl.Execute(&out, row); err != nil {
		t.Fatalf("docker ps --format would reject this template: %v", err)
	}
	if out.String() != "/clinic/install" {
		t.Fatalf("working directory = %q", out.String())
	}
}

func TestCachedImagesDoNotBlockSetup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX command fixtures")
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	docker := "#!/bin/sh\ncase \"$1 $2\" in \"images --format\") echo care:clinic ;; esac\n"
	if err := os.WriteFile(filepath.Join(root, "docker"), []byte(docker), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	report, err := scan(Options{
		Runner: proc.Runner{Env: os.Environ()}, Project: "care-clinic",
		InstallDir: filepath.Join(root, "install"), Images: []string{"care:clinic"},
	}, noSystemTraces)
	if err != nil || !report.Clean || len(report.Traces) != 1 || report.Traces[0].ID != "images" {
		t.Fatalf("cached images should be reported but not block: %+v, %v", report, err)
	}
	if err := os.MkdirAll(filepath.Join(root, "install"), 0o700); err != nil {
		t.Fatal(err)
	}
	report, err = scan(Options{
		Runner: proc.Runner{Env: os.Environ()}, Project: "care-clinic",
		InstallDir: filepath.Join(root, "install"), Images: []string{"care:clinic"},
	}, noSystemTraces)
	if err != nil || report.Clean {
		t.Fatalf("installed files should still block: %+v, %v", report, err)
	}
}

func noSystemTraces(proc.Runner) ([]Trace, error) { return nil, nil }

func TestSystemTracesClassifySetupResidue(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX command fixtures")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "docker"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	inspectionError := errors.New("hosts file could not be read")
	for _, tc := range []struct {
		name   string
		traces []Trace
		err    error
		clean  bool
	}{
		{"after cleanup", nil, nil, true},
		{"after network repair", []Trace{{ID: "firewall", Label: "Firewall rules"}}, nil, true},
		{"unknown system state", nil, inspectionError, false},
		{"firewall and inspection failure", []Trace{{ID: "firewall"}}, inspectionError, false},
		{"existing hosts entry", []Trace{{ID: "hosts", Label: "Hosts file entry"}}, nil, false},
		{"firewall and old hosts entry", []Trace{{ID: "firewall"}, {ID: "hosts"}}, nil, false},
		{"firewall and old certificate", []Trace{{ID: "firewall"}, {ID: "certificate"}}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report, err := scan(Options{
				Runner: proc.Runner{Env: os.Environ()}, Project: "care-clinic",
				InstallDir: filepath.Join(root, "install"),
			}, func(proc.Runner) ([]Trace, error) { return tc.traces, tc.err })
			if report.Clean != tc.clean || !errors.Is(err, tc.err) || len(report.Traces) != len(tc.traces) {
				t.Fatalf("system inspection was lost: %+v, %v", report, err)
			}
			if report.Traces == nil {
				t.Fatal("scan returned null traces instead of an empty array")
			}
		})
	}
}
