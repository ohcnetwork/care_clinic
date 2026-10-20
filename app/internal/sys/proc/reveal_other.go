//go:build !windows

package proc

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func Reveal(path string) error {
	var cmd *exec.Cmd
	switch {
	case runtime.GOOS == "darwin":
		cmd = Command("open", "-R", path)
	case isDir(path):
		cmd = Command("xdg-open", path)
	default:
		cmd = Command("xdg-open", filepath.Dir(path))
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
