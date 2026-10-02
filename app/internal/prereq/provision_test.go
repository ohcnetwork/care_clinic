package prereq

import (
	"errors"
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

func TestDockerWaitRetriesRancherAfterItFinishesQuitting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		var relaunchedAt time.Time
		attempts := 0
		var logs []string
		err := waitForDockerReady(dockerReadyTimeout,
			func() bool { return attempts > 0 && time.Since(relaunchedAt) >= 6*time.Second },
			func() bool { return attempts > 0 || time.Since(start) < 6*time.Second },
			func() error {
				attempts++
				relaunchedAt = time.Now()
				return nil
			},
			func(line string) { logs = append(logs, line) },
		)
		if err != nil {
			t.Fatal(err)
		}
		if attempts != 1 || relaunchedAt.Sub(start) != rancherLaunchGrace {
			t.Fatalf("relaunches = %d, first at %s; want one after the launch grace", attempts, relaunchedAt.Sub(start))
		}
		if elapsed := time.Since(start); elapsed != rancherLaunchGrace+6*time.Second {
			t.Fatalf("recovery took %s instead of detecting the stopped process promptly", elapsed)
		}
		if len(logs) != 2 || !strings.Contains(logs[0], "trying again") || logs[1] != "Docker is ready." {
			t.Fatalf("missing recovery progress: %v", logs)
		}
	})
}

func TestDockerWaitAllowsRancherTimeToAppearAndStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		err := waitForDockerReady(dockerReadyTimeout,
			func() bool { return time.Since(start) >= time.Minute },
			func() bool { return time.Since(start) >= 12*time.Second },
			func() error {
				t.Fatal("a starting Rancher instance must not be relaunched")
				return nil
			},
			func(string) {},
		)
		if err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(start); elapsed != time.Minute {
			t.Fatalf("wait returned after %s, before Docker was ready", elapsed)
		}
	})
}

func TestDockerWaitFailsWhenRancherNeverStaysRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		attempts := 0
		err := waitForDockerReady(dockerReadyTimeout,
			func() bool { return false },
			func() bool { return false },
			func() error { attempts++; return nil },
			func(string) {},
		)
		if err == nil || !strings.Contains(err.Error(), "Rancher Desktop stopped before Docker was ready") {
			t.Fatalf("missing actionable startup failure: %v", err)
		}
		if attempts != 1 {
			t.Fatalf("relaunches = %d, want exactly one", attempts)
		}
		if elapsed := time.Since(start); elapsed != 2*rancherLaunchGrace {
			t.Fatalf("a stopped Rancher instance kept the operation waiting for %s", elapsed)
		}
	})
}

func TestDockerWaitReportsRancherRelaunchFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		want := errors.New("launch was rejected")
		err := waitForDockerReady(dockerReadyTimeout,
			func() bool { return false },
			func() bool { return false },
			func() error { return want },
			func(string) {},
		)
		if !errors.Is(err, want) {
			t.Fatalf("relaunch error = %v, want %v", err, want)
		}
	})
}

func TestDockerWaitKeepsTheReadinessDeadline(t *testing.T) {
	for _, rancher := range []bool{false, true} {
		t.Run(map[bool]string{false: "Linux Docker", true: "running Rancher"}[rancher], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				var running func() bool
				var relaunch func() error
				if rancher {
					running = func() bool { return true }
					relaunch = func() error {
						t.Fatal("a running Rancher instance must not be relaunched")
						return nil
					}
				}
				err := waitForDockerReady(40*time.Second, func() bool { return false }, running, relaunch, func(string) {})
				if err == nil || !strings.Contains(err.Error(), "taking longer than usual") {
					t.Fatalf("missing readiness timeout: %v", err)
				}
				if elapsed := time.Since(start); elapsed != 40*time.Second {
					t.Fatalf("readiness wait exceeded its deadline: %s", elapsed)
				}
			})
		})
	}
}

func TestDockerWaitDoesNotRelaunchAReadyEngine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		err := waitForDockerReady(dockerReadyTimeout,
			func() bool { return true },
			func() bool { t.Fatal("a ready engine needs no process check"); return false },
			func() error { t.Fatal("a ready engine must not be relaunched"); return nil },
			func(string) {},
		)
		if err != nil || !time.Now().Equal(start) {
			t.Fatalf("ready engine should return immediately: %v", err)
		}
	})
}
