//go:build !darwin && !windows

package prereq

import "runtime"

func readHostFacts() hostFacts {
	return hostFacts{OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU(), Virtualization: virtualizationNone}
}
