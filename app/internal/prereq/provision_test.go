package prereq

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
