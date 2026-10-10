//go:build !windows

package vm

import (
	"os/exec"
	"syscall"
)

// detach lets the helper outlive the app window.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
