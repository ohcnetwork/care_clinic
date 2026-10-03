package proc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNetworkFailureDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{
		{`go: Get "https://proxy.golang.org/example/@v/v1.mod": unexpected EOF`, true},
		{`fatal: unable to access 'https://github.com/example/repo/': Could not resolve host: github.com`, true},
		{`failed to do request: Head "https://registry.example/v2/": net/http: TLS handshake timeout`, true},
		{`npm ERR! code ECONNRESET`, true},
		{`Temporary failure in name resolution`, true},
		{`dial tcp: network is unreachable`, true},
		{`download https://example.invalid/file: i/o timeout`, true},
		{`syntax error: unexpected EOF`, false},
		{`download checksum mismatch`, false},
		{`Get "https://example.invalid/private": HTTP 403`, false},
		{`Cannot connect to the Docker daemon at unix:///tmp/docker.sock`, false},
		{`no space left on device`, false},
	} {
		t.Run(tc.line, func(t *testing.T) {
			if got := networkFailure.MatchString(tc.line); got != tc.want {
				t.Fatalf("network diagnostic = %v; want %v", got, tc.want)
			}
		})
	}
}

func TestRunPreservesInterruptedDownloadCauseWithoutExposingOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	for _, redirect := range []string{"", " >&2"} {
		r := Runner{}
		err := r.Run("/bin/sh", "-c", `printf '%s\n' "$1"`+redirect+`; exit 1`,
			"download-test", `Get "https://example.invalid/private?token=test-only": unexpected EOF`)
		var network *NetworkError
		var exit *exec.ExitError
		if !errors.As(err, &network) || !errors.As(err, &exit) || exit.ExitCode() != 1 {
			t.Fatalf("the command's network and exit causes weren't preserved: %v", err)
		}
		if err.Error() != "exit status 1" {
			t.Fatalf("command output leaked into the error: %v", err)
		}
	}
}

func TestSuccessfulNetworkWarningDoesNotTaintLaterFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	r := Runner{}
	if err := r.Run("/bin/sh", "-c", `printf 'connection reset by peer; retry succeeded\n'`); err != nil {
		t.Fatalf("a successful command became a failure: %v", err)
	}
	err := r.Run("/bin/sh", "-c", "exit 1")
	var network *NetworkError
	if err == nil || errors.As(err, &network) {
		t.Fatalf("an unrelated command inherited the network failure: %v", err)
	}
}

func TestOldNetworkWarningDoesNotClassifyALaterBuildFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	err := (Runner{}).Run("/bin/sh", "-c", `printf '%s\n' "$1" "$2" "$3" >&2; exit 1`,
		"build-test", "connection reset by peer; retry succeeded",
		strings.Repeat("building the next stage\n", networkDiagnosticTail), "error: undefined variable")
	var network *NetworkError
	if err == nil || errors.As(err, &network) {
		t.Fatalf("an old recovered warning hid the real build failure: %v", err)
	}
}

func TestCancelledCommandIsNotCalledANetworkFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := Runner{Ctx: ctx, Log: func(string) { cancel() }}
	err := r.Run("/bin/sh", "-c", "printf 'connection reset by peer\\n'; exec sleep 30")
	var network *NetworkError
	if err == nil || errors.As(err, &network) {
		t.Fatalf("a cancelled command was called a network interruption: %v", err)
	}
}

func TestLinesDistinguishesFailureFromEmptyOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	dir := t.TempDir()
	for name, body := range map[string]string{
		"failed": "#!/bin/sh\nexit 1\n",
		"empty":  "#!/bin/sh\nexit 0\n",
		"lines":  "#!/bin/sh\nprintf ' first \\n\\nsecond\\n'\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	r := Runner{}
	if _, err := r.Lines(filepath.Join(dir, "failed")); err == nil {
		t.Fatal("a failed command became an empty successful result")
	}
	if lines, err := r.Lines(filepath.Join(dir, "empty")); err != nil || len(lines) != 0 {
		t.Fatalf("empty output: %v, %v", lines, err)
	}
	if lines, err := r.Lines(filepath.Join(dir, "lines")); err != nil || len(lines) != 2 || lines[0] != "first" || lines[1] != "second" {
		t.Fatalf("line parsing: %v, %v", lines, err)
	}
}

func TestRunWithPreservesInheritedEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}

	t.Setenv("CARE_INHERITED", "kept")
	if err := (Runner{}).RunWith([]string{"CARE_EXTRA=added"}, "/bin/sh", "-c",
		`test "$CARE_INHERITED" = kept && test "$CARE_EXTRA" = added`); err != nil {
		t.Fatal(err)
	}
}

func TestCancelledRunDoesNotWaitForInheritedOutputPipes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := (Runner{Ctx: ctx}).Run("/bin/sh", "-c", "sleep 1 & wait")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline was not reported: %v", err)
	}
	if time.Since(started) >= 800*time.Millisecond {
		t.Fatal("an inherited output pipe kept the cancelled command open")
	}
}
