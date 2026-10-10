//go:build windows

package vm

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// detach lets the helper outlive the app window without a console.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}
