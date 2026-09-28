package prereq

import (
	"context"
	"runtime"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
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
	missing, stopped := dockerAdvice()
	switch {
	case err == nil:
		serverOS, version, _ := strings.Cut(strings.TrimSpace(string(out)), "/")
		if problem := wrongContainerOS(serverOS); problem != "" {
			return Status{OK: false, Message: problem}
		}
		if !hasCompose(run) {
			return Status{OK: false, Message: composeAdvice()}
		}
		return Status{OK: true, Message: "Docker " + version}
	case !rancherDesktopInstalled():
		if runtime.GOOS == "windows" && !wslReady() {
			return Status{OK: false, Message: "Rancher Desktop is not installed, and WSL 2 has to be on before it can be."}
		}
		return Status{OK: false, Message: missing}
	default:
		if rancherDesktopRunning() {
			return Status{OK: false, Message: dockerName() + " is running, but Docker is not answering yet. " +
				"It may still be starting; if this does not clear, quit " + dockerName() + " and start it again."}
		}
		return Status{OK: false, Message: stopped}
	}
}

func dockerAdvice() (missing, stopped string) {
	switch runtime.GOOS {
	case "windows":
		return "Rancher Desktop is not installed.",
			"Rancher Desktop is installed but not running. If you just installed it, restart this computer."
	case "darwin":
		return "Rancher Desktop is not installed.",
			"Rancher Desktop is installed but not running."
	default:
		return "Docker is not installed.",
			"Docker is installed but not running."
	}
}

func composeAdvice() string {
	if runtime.GOOS == "linux" {
		return "Docker is running, but the Compose plugin is missing. Choose Install Docker to add it."
	}
	return "Docker is running, but the Compose v2 plugin is missing. Update Rancher Desktop."
}

func wrongContainerOS(serverOS string) string {
	if serverOS == "" || serverOS == "linux" {
		return ""
	}
	return "Docker is set to " + serverOS + " containers, and CARE needs Linux containers. " +
		"In Rancher Desktop, open Preferences and set the container engine to \"dockerd (moby)\"."
}

func hasCompose(run proc.Runner) bool {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := proc.CommandContext(ctx, "docker", "compose", "version")
	cmd.Env = run.Env
	return cmd.Run() == nil
}

func GitCheck(run proc.Runner) Status {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := proc.CommandContext(ctx, "git", "--version")
	cmd.Env = run.Env
	out, err := cmd.Output()
	if err == nil {
		return Status{OK: true, Message: strings.TrimSpace(string(out))}
	}
	return Status{OK: false, Message: "Git is not installed."}
}
