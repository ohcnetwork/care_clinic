package clinic

import (
	"runtime"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/release"
	"github.com/ohcnetwork/care_desktop/app/internal/vm"
)

func TestRunnerIgnoresTheActiveDockerContext(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DOCKER_HOST", "tcp://elsewhere:2375")
	t.Setenv("DOCKER_CONTEXT", "desktop-linux")
	e := &Clinic{InstallDir: t.TempDir(), Pins: &release.Pins{}}
	var host, context string
	for _, kv := range e.Runner().Env {
		if v, ok := strings.CutPrefix(kv, "DOCKER_HOST="); ok {
			host = v
		}
		if v, ok := strings.CutPrefix(kv, "DOCKER_CONTEXT="); ok {
			context = v
		}
	}
	switch runtime.GOOS {
	case "darwin", "windows":
		if host != vm.DockerHost() || context != "default" {
			t.Fatalf("engine not pinned to the CARE engine: DOCKER_HOST=%q DOCKER_CONTEXT=%q", host, context)
		}
	default:
		if host != "tcp://elsewhere:2375" || context != "desktop-linux" {
			t.Fatalf("linux should use the inherited engine: DOCKER_HOST=%q DOCKER_CONTEXT=%q", host, context)
		}
	}
}
