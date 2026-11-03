package prereq

import (
	"context"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

var macOSNames = map[int]string{
	11: "Big Sur", 12: "Monterey", 13: "Ventura", 14: "Sonoma", 15: "Sequoia", 26: "Tahoe",
}

func readHostFacts() hostFacts {
	f := hostFacts{OS: "darwin", Arch: "amd64", CPUs: runtime.NumCPU(), Virtualization: virtualizationNone}
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	if out, err := proc.CommandContext(ctx, "/usr/bin/sw_vers", "-productVersion").Output(); err == nil {
		version := strings.TrimSpace(string(out))
		major, _, _ := strings.Cut(version, ".")
		f.MacMajor, _ = strconv.Atoi(major)
		f.OSName = "macOS " + version
		if name := macOSNames[f.MacMajor]; name != "" {
			f.OSName += " " + name
		}
	}
	// hw.optional.arm64 reports the hardware even when this binary runs under Rosetta.
	if v, err := unix.SysctlUint32("hw.optional.arm64"); err == nil && v == 1 {
		f.Arch = "arm64"
	}
	if v, err := unix.SysctlUint64("hw.memsize"); err == nil {
		f.MemoryBytes = v
	}
	if v, err := unix.SysctlUint32("kern.hv_support"); err == nil {
		f.Virtualization = virtualizationNo
		if v == 1 {
			f.Virtualization = virtualizationYes
		}
	}
	return f
}
