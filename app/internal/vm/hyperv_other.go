//go:build !windows

package vm

// HypervisorReady is only meaningful on Windows.
func HypervisorReady() error { return nil }

func makeSparse(string) {}
