package prereq

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

// rancherDistro is the WSL distribution that runs Rancher Desktop's Docker.
const rancherDistro = "rancher-desktop"

const rancherStateTimeout = 10 * time.Second

// launcher runs Rancher Desktop commands from the home folder. The clinic
// runner works in the install folder once setup has created it, and Rancher
// hands its working directory down to its long-lived WSL helpers, which would
// then keep that folder open and make uninstalling fail with "being used by
// another process".
func (pr *Provisioner) launcher() proc.Runner {
	run := pr.run
	if home, err := os.UserHomeDir(); err == nil {
		run.Dir = home
	} else {
		run.Dir = os.TempDir()
	}
	return run
}

// RancherDown reports whether an installed Rancher Desktop has quit or is
// stuck, as opposed to running or still starting. Always false where CARE
// does not run Docker through Rancher Desktop.
func RancherDown() bool {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		return false
	}
	if !rancherDesktopInstalled() {
		return false
	}
	return !rancherDesktopRunning() || RancherStuck()
}

// RancherStuck reports whether Rancher Desktop's window is open while its
// virtual machine is gone or failed. Rancher does not recover from this by
// itself: a WSL update from Windows Update, for one, shuts the VM down under
// it, leaves vmState at ERROR, and a plain `rdctl start` is then ignored. Only
// a full shutdown and start brings Docker back.
func RancherStuck() bool {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		return false
	}
	if !rancherDesktopRunning() {
		return false
	}
	state, known := rancherVMState()
	return vmStuck(state, known, runtime.GOOS == "windows", rancherDistroRunning)
}

// vmStuck decides from Rancher's reported state, consulting WSL only when the
// state alone cannot tell. A starting or stopping VM is never stuck: those
// are Rancher's own transitions, and the caller waits them out. Neither is an
// unknown state: Rancher's API does not answer for its first moments, before
// the VM exists, and that is a normal startup.
func vmStuck(state string, known, windows bool, distroRunning func() bool) bool {
	if !known {
		return false
	}
	switch state {
	case "ERROR", "STOPPED":
		return true
	case "STARTED":
		// Rancher can still claim STARTED after WSL stopped its distribution.
		return windows && !distroRunning()
	}
	return false
}

func rancherVMState() (state string, known bool) {
	rdctl := rdctlPath()
	if rdctl == "" {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), rancherStateTimeout)
	defer cancel()
	out, err := proc.CommandContext(ctx, rdctl, "api", "/v1/backend_state").Output()
	if err != nil {
		return "", false
	}
	return parseVMState(out)
}

func parseVMState(out []byte) (string, bool) {
	var body struct {
		VMState string `json:"vmState"`
	}
	if err := json.Unmarshal(out, &body); err != nil || body.VMState == "" {
		return "", false
	}
	return strings.ToUpper(body.VMState), true
}

func rancherDistroRunning() bool {
	ctx, cancel := context.WithTimeout(context.Background(), rancherStateTimeout)
	defer cancel()
	out, err := proc.CommandContext(ctx, "wsl", "--list", "--quiet", "--running").Output()
	if err != nil {
		// "There are no running distributions" exits non-zero; so does a WSL
		// service that is mid-update. Neither is running Rancher's VM.
		return false
	}
	return distroListed(out, rancherDistro)
}

// distroListed reads wsl.exe output, which is UTF-16LE on Windows.
func distroListed(out []byte, name string) bool {
	text := strings.ReplaceAll(strings.TrimPrefix(string(out), "\xff\xfe"), "\x00", "")
	for _, line := range strings.Split(text, "\n") {
		if strings.EqualFold(strings.TrimSpace(line), name) {
			return true
		}
	}
	return false
}
