package proc

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const chunkSize = 64 * 1024

const pathMarker = "__care_path__"

// Runner executes commands with a fixed working directory, environment, and log
type Runner struct {
	Dir string
	Env []string
	Log func(string)
	Ctx context.Context
}

func (r Runner) logln(s string) {
	if r.Log != nil {
		r.Log(s)
	}
}

func Command(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	hideConsole(c)
	return c
}

func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, name, args...)
	hideConsole(c)
	return c
}

func (r Runner) cmd(name string, args ...string) *exec.Cmd {
	c := Command(name, args...)
	if r.Ctx != nil {
		c = CommandContext(r.Ctx, name, args...)
	}
	c.Dir = r.Dir
	c.Env = r.Env
	return c
}

func (r Runner) Run(name string, args ...string) error {
	return r.RunWith(nil, name, args...)
}

func (r Runner) RunWith(extraEnv []string, name string, args ...string) error {
	cmd := r.cmd(name, args...)
	if len(extraEnv) > 0 {
		cmd.Env = append(cmd.Environ(), extraEnv...)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}
	var wg sync.WaitGroup
	var outputMu sync.Mutex
	linesSinceNetworkFailure := networkDiagnosticTail

	stream := func(rd io.Reader) {
		defer wg.Done()
		br := bufio.NewReaderSize(rd, chunkSize)
		skipping := false
		for {
			chunk, isPrefix, err := br.ReadLine()
			if len(chunk) > 0 && !skipping {
				outputMu.Lock()
				if networkFailure.Match(chunk) {
					linesSinceNetworkFailure = 0
				} else if linesSinceNetworkFailure < networkDiagnosticTail {
					linesSinceNetworkFailure++
				}
				outputMu.Unlock()
				r.logln(string(chunk))
				if isPrefix {
					r.logln("  (line too long to show in full - truncated)")
				}
			}
			skipping = isPrefix
			if err != nil {
				return
			}
		}
	}
	wg.Add(2)
	go stream(stdout)
	go stream(stderr)
	wg.Wait()
	err = cmd.Wait()
	if err != nil && linesSinceNetworkFailure < networkDiagnosticTail && (r.Ctx == nil || r.Ctx.Err() == nil) {
		return &NetworkError{Err: err}
	}
	return err
}

// Capture returns trimmed stdout without streaming.
func (r Runner) Capture(name string, args ...string) (string, error) {
	out, err := r.cmd(name, args...).Output()
	return strings.TrimSpace(string(out)), err
}

func (r Runner) CaptureIn(timeout time.Duration, extraEnv []string, name string, args ...string) (string, error) {
	parent := r.Ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env = r.Dir, r.Env
	if len(extraEnv) > 0 {
		cmd.Env = append(cmd.Environ(), extraEnv...)
	}
	out, err := cmd.Output()
	if err := parent.Err(); err != nil {
		return "", fmt.Errorf("%s was stopped: %w", name, err)
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("%s timed out after %s", name, timeout)
	}
	return strings.TrimSpace(string(out)), err
}

// Lines returns Capture's non-empty output lines, trimmed.
func (r Runner) Lines(name string, args ...string) ([]string, error) {
	out, err := r.Capture(name, args...)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	if out == "" {
		return nil, nil
	}
	var lines []string
	for _, ln := range strings.Split(out, "\n") {
		if s := strings.Trim(ln, " \t\r"); s != "" {
			lines = append(lines, s)
		}
	}
	return lines, nil
}

func AugmentedPath() string {
	var parts []string
	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
		parts = []string{
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Rancher Desktop",
				"resources", "resources", "win32", "bin"),
			`C:\Program Files\Rancher Desktop\resources\resources\win32\bin`,
			`C:\Program Files\Git\bin`,
			`C:\Program Files\Git\cmd`,
		}
	} else {
		parts = []string{
			filepath.Join(os.Getenv("HOME"), ".rd", "bin"),
			"/opt/homebrew/bin", "/opt/homebrew/sbin",
			"/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin",
		}
	}
	if existing := os.Getenv("PATH"); existing != "" {
		parts = append(parts, existing)
	}
	return strings.Join(parts, sep)
}

func DockerHost() string {
	switch runtime.GOOS {
	case "darwin":
		return "unix://" + filepath.Join(os.Getenv("HOME"), ".rd", "docker.sock")
	case "windows":
		return "npipe:////./pipe/docker_engine"
	}
	return ""
}

// FixPath widens the process PATH so binary lookups succeed.
func FixPath() {
	var parts []string
	if sp := loginShellPath(); sp != "" {
		parts = append(parts, sp)
	}
	parts = append(parts, AugmentedPath())
	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}
	_ = os.Setenv("PATH", strings.Join(parts, sep))
}

func loginShellPath() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, shell, "-lc",
		"printf '"+pathMarker+"%s\\n' \"$PATH\"").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), pathMarker); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func Exists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func FileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
