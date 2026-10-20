//go:build !windows

package keepawake

import (
	"errors"
	"strings"
	"testing"
)

func TestHelperExitKeepsTheReasonTheBlockerStopped(t *testing.T) {
	err := helperExit(errors.New("exit status 1"), "Failed to inhibit: Access denied\n")
	if err == nil || !strings.Contains(err.Error(), "exit status 1") || !strings.Contains(err.Error(), "Access denied") {
		t.Fatalf("lost the helper's failure: %v", err)
	}
	if err := helperExit(nil, ""); err == nil {
		t.Fatal("a helper that exits cleanly at once still holds nothing, so it must be an error")
	}
}
