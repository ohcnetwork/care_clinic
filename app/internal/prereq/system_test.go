package prereq

import (
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/storage"
)

func goodMac() hostFacts {
	return hostFacts{OS: "darwin", OSName: "macOS 14.5 Sonoma", MacMajor: 14, Arch: "arm64",
		CPUs: 8, MemoryBytes: 16 * storage.GB, Virtualization: virtualizationYes}
}

func goodWindows() hostFacts {
	return hostFacts{OS: "windows", OSName: "Windows 11 Pro 23H2 (build 22631)", WindowsBuild: 22631, Arch: "amd64",
		CPUs: 8, MemoryBytes: 16 * storage.GB, Virtualization: virtualizationYes}
}

var roomyDisk = DiskFacts{Known: true, OK: true, Free: 200 * storage.GB, Need: storage.InstallMinFree}

func levels(r SystemReport) map[string]SystemLevel {
	out := map[string]SystemLevel{}
	for _, c := range r.Checks {
		out[c.ID] = c.Level
	}
	return out
}

func TestSystemReportPassesSupportedComputers(t *testing.T) {
	for name, facts := range map[string]hostFacts{"mac": goodMac(), "windows": goodWindows()} {
		r := evaluateSystem(facts, roomyDisk)
		if !r.Applicable || len(r.Checks) != 5 {
			t.Fatalf("%s: want 5 applicable checks, got %+v", name, r)
		}
		for _, c := range r.Checks {
			if c.Level != SystemOK || c.Message != "" {
				t.Errorf("%s: %s = %s %q", name, c.ID, c.Level, c.Message)
			}
		}
	}
}

func TestSystemReportSkipsLinux(t *testing.T) {
	if r := evaluateSystem(hostFacts{OS: "linux"}, roomyDisk); r.Applicable || len(r.Checks) != 0 {
		t.Fatalf("linux should have no checks, got %+v", r)
	}
}

func TestSystemReportWarnsWindows10(t *testing.T) {
	f := goodWindows()
	f.WindowsBuild, f.OSName = 19045, "Windows 10 Pro 22H2 (build 19045)"
	c := evaluateSystem(f, roomyDisk).Checks[0]
	if c.Level != SystemWarn || !strings.Contains(c.Message, "doesn't support Windows 10") {
		t.Fatalf("got %+v", c)
	}
}

func TestSystemReportWindowsServer(t *testing.T) {
	f := goodWindows()
	f.WindowsServer, f.WindowsBuild = true, 26100
	if l := levels(evaluateSystem(f, roomyDisk))["os"]; l != SystemOK {
		t.Fatalf("Server 2025 should pass, got %s", l)
	}
	f.WindowsBuild = 20348
	if l := levels(evaluateSystem(f, roomyDisk))["os"]; l != SystemWarn {
		t.Fatalf("Server 2022 should warn, got %s", l)
	}
}

func TestSystemReportWarnsOldMacOS(t *testing.T) {
	f := goodMac()
	f.MacMajor = 12
	if l := levels(evaluateSystem(f, roomyDisk))["os"]; l != SystemWarn {
		t.Fatalf("macOS 12 should warn, got %s", l)
	}
}

func TestSystemReportMemory(t *testing.T) {
	f := goodMac()
	f.MemoryBytes = 8*storage.GB - 300*storage.MB
	if l := levels(evaluateSystem(f, roomyDisk))["memory"]; l != SystemOK {
		t.Fatalf("an 8 GB computer with reserved memory should pass, got %s", l)
	}
	f.MemoryBytes = 4 * storage.GB
	if l := levels(evaluateSystem(f, roomyDisk))["memory"]; l != SystemWarn {
		t.Fatalf("4 GB should warn, got %s", l)
	}
	f.MemoryBytes = 0
	if l := levels(evaluateSystem(f, roomyDisk))["memory"]; l != SystemUnknown {
		t.Fatalf("unknown memory should be unknown, got %s", l)
	}
}

func TestSystemReportProcessor(t *testing.T) {
	f := goodWindows()
	f.Arch = "arm64"
	if c := evaluateSystem(f, roomyDisk).Checks[1]; c.Level != SystemWarn || !strings.Contains(c.Message, "ARM") {
		t.Fatalf("Windows on ARM should warn, got %+v", c)
	}
	f = goodMac()
	f.CPUs = 2
	if c := evaluateSystem(f, roomyDisk).Checks[1]; c.Level != SystemWarn || c.Value != "Apple Silicon · 2 cores" {
		t.Fatalf("2 cores should warn, got %+v", c)
	}
}

func TestSystemReportStorageAndVirtualization(t *testing.T) {
	f := goodWindows()
	f.Virtualization = virtualizationNo
	r := evaluateSystem(f, DiskFacts{Known: true, Free: 12 * storage.GB, Need: storage.InstallMinFree})
	got := levels(r)
	if got["storage"] != SystemWarn || got["virtualization"] != SystemWarn {
		t.Fatalf("got %v", got)
	}
	if !strings.Contains(r.Checks[4].Message, "BIOS") {
		t.Fatalf("Windows advice should mention BIOS: %q", r.Checks[4].Message)
	}
	if l := levels(evaluateSystem(goodMac(), DiskFacts{}))["storage"]; l != SystemUnknown {
		t.Fatalf("unmeasured disk should be unknown, got %s", l)
	}
}
