//go:build !windows

package atomicfile

func restrictPrivateFile(string) error { return nil }
