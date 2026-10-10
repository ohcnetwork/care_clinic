package vm

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/applog"
)

// appFolder matches the folder name used everywhere else by the app.
const appFolder = "care-clinic"

// StateDir holds the engine files, the data disk and credentials.
func StateDir() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", appFolder, "vm")
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, appFolder, "vm")
	}
	return filepath.Join(home, ".local", "share", appFolder, "vm")
}

// runDir holds sockets. macOS limits unix socket paths to 104 bytes, so it
// lives directly under the home folder there.
func runDir() string {
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "."+appFolder, "run")
	}
	return filepath.Join(StateDir(), "run")
}

func BinDir() string        { return filepath.Join(StateDir(), "bin") }
func engineDir() string     { return filepath.Join(StateDir(), "engine") }
func dataDisk() string      { return filepath.Join(StateDir(), "data.img") }
func dockerConfig() string  { return filepath.Join(StateDir(), "docker-config") }
func certDir() string       { return filepath.Join(StateDir(), "certs") }
func dockerSocket() string  { return filepath.Join(runDir(), "docker.sock") }
func controlSocket() string { return filepath.Join(runDir(), "control.sock") }
func ConsoleLog() string    { return filepath.Join(applog.DefaultLogDir(), "engine-console.log") }
func HostLog() string       { return filepath.Join(applog.DefaultLogDir(), "engine-host.log") }

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// DockerHost is the DOCKER_HOST value that reaches the engine.
func DockerHost() string {
	if runtime.GOOS == "windows" {
		return "tcp://127.0.0.1:" + strconv.Itoa(hostTCPDocker)
	}
	return "unix://" + dockerSocket()
}

// Env returns the variables that point docker and docker compose at the
// engine. It is empty where the engine is not used.
func Env() []string {
	if !Supported() {
		return nil
	}
	env := []string{
		"DOCKER_HOST=" + DockerHost(),
		"DOCKER_CONTEXT=default",
		"DOCKER_CONFIG=" + dockerConfig(),
	}
	if runtime.GOOS == "windows" {
		env = append(env,
			"DOCKER_TLS_VERIFY=1",
			"DOCKER_CERT_PATH="+certDir(),
			"COMPOSE_CONVERT_WINDOWS_PATHS=1",
		)
	}
	return env
}

// PathWithBin puts the engine's docker CLI ahead of anything else on PATH.
func PathWithBin(path string) string {
	if !Supported() {
		return path
	}
	if path == "" {
		return BinDir()
	}
	return BinDir() + string(os.PathListSeparator) + path
}

// ApplyEnv sets Env and the PATH prefix on this process so every child
// process (docker, docker compose) inherits them.
func ApplyEnv() {
	for _, kv := range Env() {
		k, v, _ := strings.Cut(kv, "=")
		_ = os.Setenv(k, v)
	}
	if Supported() && !strings.Contains(os.Getenv("PATH"), BinDir()) {
		_ = os.Setenv("PATH", PathWithBin(os.Getenv("PATH")))
	}
}
