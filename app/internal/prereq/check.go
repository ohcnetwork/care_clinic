package prereq

import (
	"context"
	"runtime"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
	"github.com/ohcnetwork/care_desktop/app/internal/vm"
)

const cmdTimeout = 5 * time.Second

type Status struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func DockerCheck(run proc.Runner) Status {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := proc.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Os}}/{{.Server.Version}}")
	cmd.Env = run.Env
	out, err := cmd.Output()
	switch {
	case err == nil:
		serverOS, version, _ := strings.Cut(strings.TrimSpace(string(out)), "/")
		if problem := wrongContainerOS(serverOS); problem != "" {
			return Status{OK: false, Message: problem}
		}
		if !hasCompose(run) {
			return Status{OK: false, Message: composeAdvice()}
		}
		if vm.Supported() {
			return Status{OK: true, Message: engineName + " (Docker " + version + ")"}
		}
		return Status{OK: true, Message: "Docker " + version}
	case vm.Supported():
		return engineCheck()
	case hasCommand("docker"):
		return Status{OK: false, Message: "Docker is installed but not running."}
	default:
		return Status{OK: false, Message: "Docker is not installed."}
	}
}

func engineCheck() Status {
	if runtime.GOOS == "windows" {
		if err := vm.HypervisorReady(); err != nil {
			return Status{OK: false, Message: "The " + engineName + " can't run yet: " + err.Error() + "."}
		}
	}
	if _, err := vm.Installed(); err != nil || !vm.ToolsInstalled() {
		return Status{OK: false, Message: "The " + engineName + " is not installed."}
	}
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	s := vm.Status(ctx)
	switch {
	case s.Error != "":
		return Status{OK: false, Message: "The " + engineName + " could not start: " + s.Error}
	case s.Running:
		return Status{OK: false, Message: "The " + engineName + " is starting. This usually takes under a minute."}
	default:
		return Status{OK: false, Message: "The " + engineName + " is installed but not running."}
	}
}

func composeAdvice() string {
	if runtime.GOOS == "linux" {
		return "Docker is running, but the Compose plugin is missing. Choose Install Docker to add it."
	}
	return "The " + engineName + " is running, but the Compose plugin is missing. Choose Update " + engineName + " to add it."
}

func wrongContainerOS(serverOS string) string {
	if serverOS == "" || serverOS == "linux" {
		return ""
	}
	return "Docker is set to " + serverOS + " containers, and CARE needs Linux containers."
}

func hasCompose(run proc.Runner) bool {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := proc.CommandContext(ctx, "docker", "compose", "version")
	cmd.Env = run.Env
	return cmd.Run() == nil
}
