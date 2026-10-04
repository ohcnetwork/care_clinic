package residue

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/autostart"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/hosts"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/netfix"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/trust"
)

type Trace struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
}

type Report struct {
	Clean  bool    `json:"clean"`
	Traces []Trace `json:"traces"`
}

type Options struct {
	Runner     proc.Runner
	Project    string
	InstallDir string
	ConfigPath string
	Images     []string
}

func Scan(o Options) (Report, error) {
	return scan(o, systemTraces)
}

// engineAvailable separates "this computer has no Docker" from "Docker is
// installed but not answering". A client, or a computer where setup never got
// as far as installing an engine, has no Docker-side traces to find; that is an
// empty result, not an inspection failure.
func engineAvailable(run proc.Runner) (bool, error) {
	if _, err := run.Capture("docker", "--version"); err != nil {
		if missingCommand(err) {
			return false, nil
		}
		return false, fmt.Errorf("could not inspect this computer's clinic containers; start Docker and try again: %w", err)
	}
	return true, nil
}

func missingCommand(err error) bool {
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist)
}

func scan(o Options, inspectSystem func(proc.Runner) ([]Trace, error)) (Report, error) {
	traces := []Trace{}
	var failed []error
	add := func(id, label, detail string) {
		traces = append(traces, Trace{ID: id, Label: label, Detail: detail})
	}

	engine, err := engineAvailable(o.Runner)
	if err != nil {
		failed = append(failed, err)
	}
	if engine {
		docker, dockerErr := dockerTraces(o)
		traces = append(traces, docker...)
		if dockerErr != nil {
			failed = append(failed, dockerErr)
		}
	}

	if _, err := os.Stat(o.InstallDir); err == nil {
		add("install-dir", "Installed files", o.InstallDir)
	} else if !os.IsNotExist(err) {
		failed = append(failed, fmt.Errorf("could not inspect installed files: %w", err))
	}
	if o.ConfigPath != "" {
		if _, err := os.Stat(o.ConfigPath); err == nil {
			add("config", "Saved settings", o.ConfigPath)
		} else if !os.IsNotExist(err) {
			failed = append(failed, fmt.Errorf("could not inspect saved settings: %w", err))
		}
	}

	system, err := inspectSystem(o.Runner)
	traces = append(traces, system...)
	if err != nil {
		failed = append(failed, err)
	}

	blocking := 0
	for _, trace := range traces {
		// Network repair creates firewall rules before setup; they are not an old clinic.
		if trace.ID != "images" && trace.ID != "firewall" {
			blocking++
		}
	}
	return Report{Clean: blocking == 0 && len(failed) == 0, Traces: traces}, errors.Join(failed...)
}

func dockerTraces(o Options) ([]Trace, error) {
	var traces []Trace
	var failed []error
	add := func(id, label, detail string) {
		traces = append(traces, Trace{ID: id, Label: label, Detail: detail})
	}

	label := "label=com.docker.compose.project=" + o.Project

	for _, resource := range []struct {
		id, label, one, many string
		args                 []string
	}{
		{"containers", "Clinic containers", "container", "containers", []string{"ps", "-aq", "--filter", label}},
		{"volumes", "Old clinic data", "data volume", "data volumes", []string{"volume", "ls", "-q", "--filter", label}},
		{"networks", "Clinic network", "network", "networks", []string{"network", "ls", "-q", "--filter", label}},
	} {
		ids, err := o.Runner.Lines("docker", resource.args...)
		if err != nil {
			failed = append(failed, fmt.Errorf("could not inspect %s; start Docker and try again: %w", resource.label, err))
		} else if len(ids) > 0 {
			add(resource.id, resource.label, plural(len(ids), resource.one, resource.many)+" from an earlier install")
		}
	}
	images, err := presentImages(o.Runner, o.Images)
	if err != nil {
		failed = append(failed, err)
	} else if len(images) > 0 {
		add("images", "Clinic images", plural(len(images), "Docker image", "Docker images")+" from an earlier install")
	}
	return traces, errors.Join(failed...)
}

func systemTraces(run proc.Runner) ([]Trace, error) {
	var traces []Trace
	var failed []error
	add := func(id, label, detail string) {
		traces = append(traces, Trace{ID: id, Label: label, Detail: detail})
	}
	hostsEntry, err := hosts.Inspect()
	if err != nil {
		failed = append(failed, err)
	} else if hostsEntry {
		add("hosts", "Hosts file entry", "this computer still resolves the old clinic address to itself")
	}
	trusted, err := trust.Inspect()
	if err != nil {
		failed = append(failed, err)
	} else if trusted {
		add("certificate", "Security certificate", "this computer still trusts the old clinic's certificate")
	}
	firewall, err := netfix.InspectRules(run)
	if err != nil {
		failed = append(failed, err)
	} else if firewall {
		add("firewall", "Firewall rules", "the clinic's inbound rules are still in place")
	}
	if autostart.Enabled() {
		add("autostart", "Start at login", "CARE Clinic is set to open when this computer starts")
	}
	return traces, errors.Join(failed...)
}

const workingDirFormat = `{{.Label "com.docker.compose.project.working_dir"}}`

func InstallDirFrom(run proc.Runner, project, configured string) (string, error) {
	found, err := hasComposeFile(configured)
	if err != nil || found {
		return configured, err
	}
	engine, err := engineAvailable(run)
	if err != nil || !engine {
		return configured, err
	}
	dirs, err := run.Lines("docker", "ps", "-a",
		"--filter", "label=com.docker.compose.project="+project,
		"--format", workingDirFormat)
	if err != nil {
		return configured, fmt.Errorf("could not locate the earlier installation; start Docker and try again: %w", err)
	}
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		found, err := hasComposeFile(dir)
		if err != nil {
			return configured, err
		}
		if found {
			return dir, nil
		}
	}
	return configured, nil
}

func hasComposeFile(dir string) (bool, error) {
	if dir == "" {
		return false, nil
	}
	info, err := os.Stat(filepath.Join(dir, "docker-compose.yml"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("the compose file in %s is not a regular file", dir)
	}
	return true, nil
}

func presentImages(run proc.Runner, tags []string) ([]string, error) {
	have := map[string]bool{}
	lines, err := run.Lines("docker", "images", "--format", "{{.Repository}}:{{.Tag}}")
	if err != nil {
		return nil, fmt.Errorf("could not inspect Docker images: %w", err)
	}
	for _, line := range lines {
		have[strings.TrimSpace(line)] = true
	}
	var found []string
	for _, t := range tags {
		if have[t] {
			found = append(found, t)
		}
	}
	return found, nil
}

func plural(n int, one, many string) string {
	word := many
	if n == 1 {
		word = one
	}
	return strconv.Itoa(n) + " " + word
}
