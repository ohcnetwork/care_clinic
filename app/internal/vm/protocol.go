// Package vm runs the CARE engine: a small Linux VM that hosts Docker for the
// clinic on macOS (Virtualization.framework) and Windows (QEMU with the
// Windows Hypervisor Platform). Linux keeps using the system's own Docker.
//
// The rest of the app keeps talking to Docker through the normal docker and
// docker compose CLIs; this package only points them at the VM (see Env) and
// keeps the VM running through a small helper process (see RunHost).
package vm

import "runtime"

// Ports are shared with vm/agent/main.go; keep them in step.
const (
	vsockDocker  = 2375
	vsockControl = 2377
	vsockHTTP    = 80
	vsockHTTPS   = 443
)

// Fixed loopback ports QEMU forwards on Windows. They only listen on
// 127.0.0.1 and require the per-install client certificate.
const (
	hostTCPDocker  = 47375
	hostTCPControl = 47377
)

// HostFlag starts the helper process that owns the VM.
const HostFlag = "--vm-host"

// Supported reports whether this OS runs Docker inside the CARE engine.
func Supported() bool { return supportedOS(runtime.GOOS) }

func supportedOS(goos string) bool { return goos == "darwin" || goos == "windows" }
