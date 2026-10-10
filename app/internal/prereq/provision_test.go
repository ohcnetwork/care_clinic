package prereq

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

func TestDockerDaemonProbeIsBoundedAndPreservesEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
[ "$1" = version ] && [ "$2" = --format ] && [ "$3" = '{{.Server.Version}}' ] || exit 90
case "$CARE_DOCKER_PROBE" in
  up) exit 0 ;;
  down) exit 1 ;;
  hang) exec sleep 30 ;;
  *) exit 91 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, tc := range []struct {
		state string
		want  bool
	}{
		{"up", true},
		{"down", false},
		{"hang", false},
	} {
		t.Run(tc.state, func(t *testing.T) {
			pr := NewProvisioner(proc.Runner{Env: append(os.Environ(), "CARE_DOCKER_PROBE="+tc.state)}, nil, nil)
			start := time.Now()
			if got := pr.dockerDaemonUp(); got != tc.want {
				t.Fatalf("daemon up = %v, want %v", got, tc.want)
			}
			if elapsed := time.Since(start); elapsed > cmdTimeout+3*time.Second {
				t.Fatalf("probe exceeded its timeout: %s", elapsed)
			}
		})
	}
}

func TestDockerWaitKeepsTheReadinessDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		err := waitForDockerReady(40*time.Second, func() bool { return false }, func(string) {})
		if err == nil || !strings.Contains(err.Error(), "taking longer than usual") {
			t.Fatalf("missing readiness timeout: %v", err)
		}
		if elapsed := time.Since(start); elapsed != 40*time.Second {
			t.Fatalf("readiness wait exceeded its deadline: %s", elapsed)
		}
	})
}

func TestDockerWaitReturnsOnceReady(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		var logs []string
		err := waitForDockerReady(dockerReadyTimeout,
			func() bool { return time.Since(start) >= time.Minute },
			func(line string) { logs = append(logs, line) })
		if err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(start); elapsed < time.Minute || elapsed > time.Minute+3*time.Second {
			t.Fatalf("wait returned after %s", elapsed)
		}
		if len(logs) != 1 || logs[0] != "Docker is ready." {
			t.Fatalf("unexpected progress: %v", logs)
		}
	})
}
