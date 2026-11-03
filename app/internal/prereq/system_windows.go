package prereq

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

const (
	machineAMD64 = 0x8664
	machineARM64 = 0xAA64
)

var procInstalledMemory = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetPhysicallyInstalledSystemMemory")

func readHostFacts() hostFacts {
	f := hostFacts{OS: "windows", Arch: runtime.GOARCH, CPUs: runtime.NumCPU(), Virtualization: virtualizationNone}
	v := windows.RtlGetVersion()
	f.WindowsBuild = int(v.BuildNumber)
	f.WindowsServer = v.ProductType != 1 // VER_NT_WORKSTATION
	f.OSName = windowsName(f.WindowsBuild)

	// An x64 build runs emulated on ARM, so ask for the machine itself.
	var process, native uint16
	if err := windows.IsWow64Process2(windows.CurrentProcess(), &process, &native); err == nil {
		switch native {
		case machineAMD64:
			f.Arch = "amd64"
		case machineARM64:
			f.Arch = "arm64"
		}
	}

	var kb uint64
	if procInstalledMemory.Find() == nil {
		if ok, _, _ := procInstalledMemory.Call(uintptr(unsafe.Pointer(&kb))); ok != 0 {
			f.MemoryBytes = kb * 1024
		}
	}

	f.Virtualization = windowsVirtualization()
	return f
}

func windowsName(build int) string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	name := ""
	display := ""
	if err == nil {
		name, _, _ = k.GetStringValue("ProductName")
		display, _, _ = k.GetStringValue("DisplayVersion")
		k.Close()
	}
	// Windows 11 still reports "Windows 10" as its ProductName.
	if build >= windows11Build {
		name = strings.Replace(name, "Windows 10", "Windows 11", 1)
	}
	if name == "" {
		name = "Windows"
	}
	if display != "" {
		name += " " + display
	}
	if build > 0 {
		name += fmt.Sprintf(" (build %d)", build)
	}
	return name
}

// A running hypervisor hides the firmware flag, so it counts as turned on.
func windowsVirtualization() int {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := proc.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
		`$c = Get-CimInstance Win32_ComputerSystem; `+
			`$p = Get-CimInstance Win32_Processor | Select-Object -First 1; `+
			`"$($c.HypervisorPresent)|$($p.VirtualizationFirmwareEnabled)"`).Output()
	if err != nil {
		return virtualizationNone
	}
	hypervisor, firmware, _ := strings.Cut(strings.TrimSpace(string(out)), "|")
	switch {
	case strings.EqualFold(hypervisor, "True"), strings.EqualFold(firmware, "True"):
		return virtualizationYes
	case strings.EqualFold(firmware, "False"):
		return virtualizationNo
	}
	return virtualizationNone
}
