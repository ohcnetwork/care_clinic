package prereq

import (
	"fmt"
	"strings"

	"github.com/ohcnetwork/care_desktop/app/internal/storage"
)

// Rancher Desktop recommends 8 GB of memory and 4 CPUs. The deployment profile
// gives its VM rancherMemoryGB on macOS; WSL 2 takes half of the host's memory
// on Windows, so 8 GB is what leaves both the clinic and the OS enough room.
const (
	MinHostMemoryGB = 8
	MinHostCPUs     = 4
)

// First Windows 11 build, and the first Windows Server 2025 build.
const (
	windows11Build     = 22000
	windowsServer2025  = 26100
	minMacOSMajor      = 13
	virtualizationNo   = 0
	virtualizationYes  = 1
	virtualizationNone = -1
)

type SystemLevel string

const (
	SystemOK      SystemLevel = "ok"
	SystemWarn    SystemLevel = "warn"
	SystemUnknown SystemLevel = "unknown"
)

type SystemCheck struct {
	ID          string      `json:"id"`
	Label       string      `json:"label"`
	Value       string      `json:"value"`
	Requirement string      `json:"requirement"`
	Level       SystemLevel `json:"level"`
	Message     string      `json:"message"`
}

// SystemReport describes whether this computer meets what CARE expects. It
// never blocks anything: a computer that falls short only gets a warning.
type SystemReport struct {
	Applicable bool          `json:"applicable"`
	Checks     []SystemCheck `json:"checks"`
}

type hostFacts struct {
	OS             string
	OSName         string
	MacMajor       int
	WindowsBuild   int
	WindowsServer  bool
	Arch           string
	CPUs           int
	MemoryBytes    uint64
	Virtualization int
}

type DiskFacts struct {
	Known bool
	OK    bool
	Free  uint64
	Need  uint64
}

func SystemCheckReport(disk DiskFacts) SystemReport {
	return evaluateSystem(readHostFacts(), disk)
}

func evaluateSystem(f hostFacts, disk DiskFacts) SystemReport {
	if f.OS != "darwin" && f.OS != "windows" {
		return SystemReport{Checks: []SystemCheck{}}
	}
	return SystemReport{Applicable: true, Checks: []SystemCheck{
		osCheck(f),
		processorCheck(f),
		memoryCheck(f),
		storageCheck(disk),
		virtualizationCheck(f),
	}}
}

func osCheck(f hostFacts) SystemCheck {
	c := SystemCheck{ID: "os", Label: "Operating system", Value: f.OSName,
		Requirement: "macOS 13 Ventura or later", Level: SystemOK}
	if f.OS == "windows" {
		c.Requirement = "Windows 11"
	}
	if c.Value == "" {
		c.Value = "Unknown"
	}
	switch f.OS {
	case "darwin":
		switch {
		case f.MacMajor == 0:
			c.Level, c.Message = SystemUnknown, "Couldn't read the macOS version."
		case f.MacMajor < minMacOSMajor:
			c.Level = SystemWarn
			c.Message = "Rancher Desktop, which runs CARE on this Mac, needs macOS 13 Ventura or later. " +
				"Update macOS from System Settings before relying on CARE here."
		}
	case "windows":
		switch {
		case f.WindowsBuild == 0:
			c.Level, c.Message = SystemUnknown, "Couldn't read the Windows version."
		case f.WindowsServer && f.WindowsBuild >= windowsServer2025:
		case f.WindowsServer:
			c.Level = SystemWarn
			c.Message = "Rancher Desktop supports Windows Server 2025 only. CARE may not install or run correctly on this server."
		case f.WindowsBuild < windows11Build:
			c.Level = SystemWarn
			c.Message = "Rancher Desktop, which runs CARE on Windows, doesn't support Windows 10. " +
				"CARE may install, but it can fail or stop working after an update. Upgrade to Windows 11 if you can."
		}
	}
	return c
}

func processorCheck(f hostFacts) SystemCheck {
	c := SystemCheck{ID: "processor", Label: "Processor", Level: SystemOK,
		Requirement: fmt.Sprintf("%d or more cores", MinHostCPUs)}
	var arch string
	var problems []string
	switch {
	case f.OS == "darwin" && f.Arch == "arm64":
		arch = "Apple Silicon"
	case f.OS == "darwin":
		arch = "Intel"
	case f.Arch == "amd64":
		arch = "64-bit (x64)"
		c.Requirement = "64-bit Intel or AMD, " + c.Requirement
	case f.Arch == "arm64":
		arch = "ARM"
		c.Requirement = "64-bit Intel or AMD, " + c.Requirement
		problems = append(problems, "Rancher Desktop has no version for Windows on ARM processors, so CARE can't run its services here.")
	default:
		arch = f.Arch
		c.Requirement = "64-bit Intel or AMD, " + c.Requirement
		problems = append(problems, "This processor type isn't supported by Rancher Desktop.")
	}
	cores := "Unknown cores"
	if f.CPUs > 0 {
		cores = plural(f.CPUs, "core")
		if f.CPUs < MinHostCPUs {
			problems = append(problems, fmt.Sprintf("CARE works best with %d or more cores. With fewer, it may be slow to start and respond.", MinHostCPUs))
		}
	}
	c.Value = arch + " · " + cores
	if len(problems) > 0 {
		c.Level, c.Message = SystemWarn, strings.Join(problems, " ")
	}
	return c
}

func memoryCheck(f hostFacts) SystemCheck {
	c := SystemCheck{ID: "memory", Label: "Memory (RAM)", Level: SystemOK,
		Requirement: fmt.Sprintf("%d GB or more", MinHostMemoryGB)}
	if f.MemoryBytes == 0 {
		c.Value, c.Level, c.Message = "Unknown", SystemUnknown, "Couldn't read how much memory this computer has."
		return c
	}
	c.Value = storage.Human(f.MemoryBytes)
	// A few hundred megabytes are often reserved by firmware or graphics, so an
	// "8 GB" computer can report slightly less.
	if f.MemoryBytes+512*storage.MB < MinHostMemoryGB*storage.GB {
		c.Level = SystemWarn
		c.Message = fmt.Sprintf("CARE needs at least %d GB of memory to run smoothly. "+
			"With %s it may be slow, or stop when the clinic is busy.", MinHostMemoryGB, c.Value)
	}
	return c
}

func storageCheck(d DiskFacts) SystemCheck {
	need := d.Need
	if need == 0 {
		need = storage.InstallMinFree
	}
	c := SystemCheck{ID: "storage", Label: "Free storage", Level: SystemOK,
		Requirement: storage.Human(need) + " or more free"}
	switch {
	case !d.Known:
		c.Value, c.Level, c.Message = "Unknown", SystemUnknown, "Couldn't measure the free space on this computer."
	case !d.OK:
		c.Value = storage.Human(d.Free) + " free"
		c.Level = SystemWarn
		c.Message = fmt.Sprintf("CARE needs at least %s free to install, and more as the clinic's records grow. "+
			"Free up space before setting up CARE.", storage.Human(need))
	default:
		c.Value = storage.Human(d.Free) + " free"
	}
	return c
}

func virtualizationCheck(f hostFacts) SystemCheck {
	c := SystemCheck{ID: "virtualization", Label: "Virtualization", Requirement: "Turned on", Level: SystemOK}
	switch f.Virtualization {
	case virtualizationYes:
		c.Value = "Turned on"
	case virtualizationNo:
		c.Value, c.Level = "Turned off", SystemWarn
		c.Message = "Rancher Desktop runs CARE in a virtual machine, which needs virtualization. "
		if f.OS == "windows" {
			c.Message += "Turn on Intel VT-x or AMD-V (SVM) in this computer's BIOS or UEFI settings."
		} else {
			c.Message += "This Mac doesn't report support for it."
		}
	default:
		c.Value, c.Level = "Unknown", SystemUnknown
		c.Message = "Couldn't check whether virtualization is turned on. Setup will tell you if it's missing."
	}
	return c
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
