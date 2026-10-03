//go:build !windows

package proc

import (
	"os"
	"path/filepath"
)

func DesktopDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Desktop"), nil
}
