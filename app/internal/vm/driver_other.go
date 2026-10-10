//go:build !darwin && !windows

package vm

func boot(bootOptions) (machine, error) { return nil, errUnsupported }
