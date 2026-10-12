package prereq

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/release"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/reboot"
	"github.com/ohcnetwork/care_desktop/app/internal/vm"
)

// engineName is what the operator sees for the Linux virtual machine that
// runs Docker on macOS and Windows.
const engineName = "CARE engine"

const (
	dockerMacStatic = "https://download.docker.com/mac/static/stable/"
	dockerWinStatic = "https://download.docker.com/win/static/stable/x86_64/"
	engineStopWait  = 2 * time.Minute
)

// engineDownloads lists everything the CARE engine needs on this platform:
// the VM image, the docker CLI, the compose plugin and, on Windows, QEMU.
func engineDownloads(p *release.Pins, goos, goarch string) ([]Download, error) {
	if p == nil {
		return nil, errors.New("this build of CARE Clinic has no download pins")
	}
	var vmArch, vmSum, dockerURL, dockerSum, composeName, composeSum string
	switch {
	case goos == "darwin" && goarch == "arm64":
		vmArch, vmSum = "arm64", p.CareVMAarch64SHA256
		dockerURL, dockerSum = dockerMacStatic+"aarch64/docker-"+p.DockerCLIVersion+".tgz", p.DockerCLIMacAarch64SHA256
		composeName, composeSum = "docker-compose-darwin-aarch64", p.ComposeMacAarch64SHA256
	case goos == "darwin" && goarch == "amd64":
		vmArch, vmSum = "amd64", p.CareVMX8664SHA256
		dockerURL, dockerSum = dockerMacStatic+"x86_64/docker-"+p.DockerCLIVersion+".tgz", p.DockerCLIMacX8664SHA256
		composeName, composeSum = "docker-compose-darwin-x86_64", p.ComposeMacX8664SHA256
	case goos == "windows" && goarch == "amd64":
		vmArch, vmSum = "amd64", p.CareVMX8664SHA256
		dockerURL, dockerSum = dockerWinStatic+"docker-"+p.DockerCLIVersion+".zip", p.DockerCLIWindowsSHA256
		composeName, composeSum = "docker-compose-windows-x86_64.exe", p.ComposeWindowsSHA256
	default:
		return nil, fmt.Errorf("the %s does not run on %s/%s", engineName, goos, goarch)
	}
	base := strings.TrimRight(p.CareVMBaseURL, "/") + "/"
	vmName := "care-engine-" + p.CareVMVersion + "-linux-" + vmArch + ".tar.gz"
	all := []Download{
		{Name: vmName, URL: base + vmName, SHA256: vmSum},
		{Name: dockerURL[strings.LastIndexByte(dockerURL, '/')+1:], URL: dockerURL, SHA256: dockerSum},
		{Name: composeName, URL: composeReleases + p.ComposeLinuxVersion + "/" + composeName, SHA256: composeSum},
	}
	if goos == "windows" {
		qemuName := "qemu-" + p.QEMUWindowsVersion + "-windows-x86_64.zip"
		all = append(all, Download{Name: qemuName, URL: base + qemuName, SHA256: p.QEMUWindowsSHA256})
	}
	return all, nil
}

// EngineDownloadInfo reports the total download size before the operator agrees to it.
func (pr *Provisioner) EngineDownloadInfo() (DownloadInfo, error) {
	downloads, err := engineDownloads(pr.pins, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return DownloadInfo{}, err
	}
	total := DownloadInfo{Name: engineName}
	for _, d := range downloads {
		info, err := inspectDownload(d)
		if err != nil {
			return DownloadInfo{}, err
		}
		total.Size += info.Size
	}
	return total, nil
}

// engineCurrent reports whether the installed engine is the version this app
// was released with, along with all its tools.
func (pr *Provisioner) engineCurrent() (installed, current bool) {
	m, err := vm.Installed()
	if err != nil {
		return false, false
	}
	return true, vm.ToolsInstalled() && (pr.pins == nil || m.Version == pr.pins.CareVMVersion)
}

// EngineVersions reports the installed CARE engine version (empty when none is
// installed) and the version this build of CARE Clinic was released with.
func (pr *Provisioner) EngineVersions() (installed, expected string, toolsOK bool) {
	if pr.pins != nil {
		expected = pr.pins.CareVMVersion
	}
	if m, err := vm.Installed(); err == nil {
		installed = m.Version
	}
	return installed, expected, vm.ToolsInstalled()
}

// UpdateEngine replaces an installed CARE engine with the pinned version. Every
// download is verified and unpacked before beforeStop runs and the running
// engine is stopped, so a failed download leaves the clinic untouched. If the
// new engine does not start, the previous engine and tools are restored. The
// data disk is kept as it is and never rolled back.
func (pr *Provisioner) UpdateEngine(beforeStop func() error) error {
	return pr.installEngineThen(beforeStop)
}

func (pr *Provisioner) enginePlan() ToolPlan {
	p := ToolPlan{URL: dockerHelpURL()}
	if runtime.GOOS == "windows" {
		if err := vm.HypervisorReady(); err != nil {
			return p
		}
	}
	installed, current := pr.engineCurrent()
	switch {
	case !installed:
		p.Action, p.Label, p.DownloadPreview = ActionInstall, "Install "+engineName, true
		p.Detail = "Downloads the " + engineName + " - a small Linux system that runs CARE's containers - " +
			"and starts it. No password is needed. Keep this computer connected to the internet."
	case !current:
		p.Action, p.Label, p.DownloadPreview = ActionInstall, "Update "+engineName, true
		p.Detail = "Downloads the " + engineName + " that matches this version of CARE Clinic and restarts it. " +
			"The clinic's data is kept."
	default:
		p.Action, p.Label = ActionOpen, "Start "+engineName
		p.Detail = "Starts the " + engineName + " and waits for it to be ready. This usually takes under a minute."
	}
	return p
}

func (pr *Provisioner) installEngine() error { return pr.installEngineThen(nil) }

// engineOps are the VM operations an engine update performs; tests replace them.
type engineOps struct {
	running func(context.Context) bool
	stop    func(context.Context) error
	boot    func() error
	version func(context.Context) string
}

func (pr *Provisioner) engineOps() engineOps {
	if pr.testEngineOps != nil {
		return *pr.testEngineOps
	}
	return engineOps{
		running: func(ctx context.Context) bool { return vm.Status(ctx).PID != 0 },
		stop:    vm.Stop,
		boot:    pr.bootEngine,
		version: func(ctx context.Context) string { return vm.Status(ctx).Version },
	}
}

// installEngineThen installs or updates the CARE engine. Every download is
// verified and unpacked before beforeStop runs and the running engine is
// stopped. An update is only kept once the new engine and Docker answer;
// otherwise the previous engine and tools are put back.
func (pr *Provisioner) installEngineThen(beforeStop func() error) error {
	if err := pr.RecoverEngineUpdate(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		if err := vm.HypervisorReady(); err != nil {
			return fmt.Errorf("%w; finish the Windows step first", err)
		}
	}
	downloads, err := engineDownloads(pr.pins, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	files := make([]string, 0, len(downloads))
	defer func() {
		for _, f := range files {
			_ = os.Remove(f)
		}
	}()
	for _, d := range downloads {
		f, err := pr.download(d)
		if err != nil {
			return err
		}
		files = append(files, f)
	}
	ef := vm.EngineFiles{Engine: files[0], Docker: files[1], Compose: files[2]}
	if len(files) > 3 {
		ef.QEMU = files[3]
	}
	pr.logln("Preparing the " + engineName + "...")
	staged, err := vm.StageEngineUpdate(ef)
	if err != nil {
		return fmt.Errorf("could not prepare the %s: %w", engineName, err)
	}
	return pr.applyEngine(staged, beforeStop)
}

func (pr *Provisioner) applyEngine(s *vm.StagedUpdate, beforeStop func() error) error {
	ops := pr.engineOps()
	if beforeStop != nil {
		if err := beforeStop(); err != nil {
			s.Discard()
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), engineStopWait)
	defer cancel()
	if ops.running(ctx) {
		pr.logln("Stopping the " + engineName + " to update it...")
		if err := ops.stop(ctx); err != nil {
			s.Discard()
			return fmt.Errorf("could not stop the %s to update it: %w", engineName, err)
		}
	}
	pr.logln("Installing the " + engineName + "...")
	if err := s.Activate(); err != nil {
		return pr.rollbackEngine(s.From, fmt.Errorf("could not install the %s: %w", engineName, err))
	}
	if s.From == "" {
		// A first install has no previous engine to go back to.
		if err := vm.CommitEngineUpdate(); err != nil {
			return err
		}
		pr.logln(engineName + " " + s.Manifest.Version + " installed.")
		return ops.boot()
	}
	pr.logln(engineName + " " + s.Manifest.Version + " installed; checking that it starts...")
	if err := ops.boot(); err != nil {
		return pr.rollbackEngine(s.From, err)
	}
	vctx, vcancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer vcancel()
	if got := ops.version(vctx); got != s.Manifest.Version {
		return pr.rollbackEngine(s.From, fmt.Errorf("the %s reported version %q after the update instead of %s",
			engineName, got, s.Manifest.Version))
	}
	if err := vm.CommitEngineUpdate(); err != nil {
		return pr.rollbackEngine(s.From, fmt.Errorf("could not finish the %s update: %w", engineName, err))
	}
	pr.logln(engineName + " " + s.Manifest.Version + " is running; the previous engine was removed.")
	return nil
}

// rollbackEngine stops whatever part of the new engine started and puts the
// previous engine and tools back (or, on a first install, removes the partial
// install). The error names both the update failure and, if it happened, the
// failure to restore.
func (pr *Provisioner) rollbackEngine(from string, cause error) error {
	pr.logln("The " + engineName + " update failed: " + cause.Error())
	what := engineName + " " + from
	if from == "" {
		what = "the state before the " + engineName + " was installed"
	}
	pr.logln("Restoring " + what + "...")
	if err := pr.restorePreviousEngine(cause.Error()); err != nil {
		pr.logln("Restoring " + what + " failed: " + err.Error())
		return fmt.Errorf("%w; restoring %s also failed (%v) - it will be tried again before CARE starts",
			cause, what, err)
	}
	pr.logln("Restored " + what + ".")
	return fmt.Errorf("%w; restored %s", cause, what)
}

func (pr *Provisioner) restorePreviousEngine(cause string) error {
	if err := vm.BeginEngineRollback(cause); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), engineStopWait)
	defer cancel()
	if err := pr.engineOps().stop(ctx); err != nil {
		return fmt.Errorf("could not stop the updated %s: %w", engineName, err)
	}
	return vm.RollbackEngineUpdate(cause)
}

// RecoverEngineUpdate rolls back an engine update that was interrupted, for
// example by the computer losing power, before the engine is started again.
func (pr *Provisioner) RecoverEngineUpdate() error {
	if !vm.Supported() {
		return nil
	}
	u, ok, err := vm.PendingEngineUpdate()
	if err != nil || !ok || !u.Interrupted() {
		return err
	}
	pr.logln(fmt.Sprintf("The update of the %s from %s to %s didn't finish; restoring %s...",
		engineName, u.From, u.To, u.From))
	if err := pr.restorePreviousEngine("the update was interrupted before it finished"); err != nil {
		return fmt.Errorf("the interrupted %s update could not be rolled back: %w", engineName, err)
	}
	pr.logln(engineName + " " + u.From + " was restored.")
	return nil
}

func (pr *Provisioner) startEngine() error {
	if err := pr.RecoverEngineUpdate(); err != nil {
		return err
	}
	return pr.bootEngine()
}

func (pr *Provisioner) bootEngine() error {
	pr.logln("Starting the " + engineName + "...")
	ctx, cancel := context.WithTimeout(context.Background(), dockerReadyTimeout)
	defer cancel()
	if err := vm.Ensure(ctx, pr.logln); err != nil {
		pr.saveEngineDiagnostics()
		return err
	}
	return pr.waitForDocker(time.Minute)
}

// WindowsVMStatus is the Windows-only requirement row: the CARE engine runs
// on Windows' own hypervisor, which is an optional Windows feature.
type WindowsVMStatus struct {
	Applicable bool   `json:"applicable"`
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
	How        string `json:"how"`
	Fixable    bool   `json:"fixable"`
}

const hypervisorFeature = "HypervisorPlatform"

func WindowsVMCheck() WindowsVMStatus {
	if runtime.GOOS != "windows" {
		return WindowsVMStatus{Applicable: false, OK: true}
	}
	err := vm.HypervisorReady()
	if err == nil {
		return WindowsVMStatus{Applicable: true, OK: true, Message: "Windows Hypervisor Platform is on"}
	}
	status := WindowsVMStatus{
		Applicable: true, Fixable: true,
		Message: "Windows Hypervisor Platform is off",
		How: "The " + engineName + " runs on Windows' built-in hypervisor. " +
			"Click Turn on; Windows will ask for permission and then need a restart.",
	}
	if strings.Contains(err.Error(), "firmware") {
		status.Message, status.Fixable = "Virtualization is turned off in this computer's firmware", false
		status.How = "Restart into the BIOS/UEFI settings, turn on Intel VT-x or AMD-V (SVM), save, and start Windows again."
	}
	if reboot.Check().Needed {
		status.Message += ", and Windows is waiting for a restart"
		status.How = "Restart this computer and run the check again. If it is still off after the restart, click Turn on."
	}
	return status
}

func (pr *Provisioner) EnableWindowsVM() (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("the Windows Hypervisor Platform is only used on Windows, not %s", runtime.GOOS)
	}
	pr.logln("Turning on the Windows Hypervisor Platform, which the " + engineName + " needs...")
	if err := pr.runElevated("dism.exe", "/online", "/enable-feature",
		"/featurename:"+hypervisorFeature, "/all", "/norestart"); err != nil && !restartRequired(err) {
		return "", fmt.Errorf("could not turn on the Windows Hypervisor Platform; turn it on from "+
			"Control Panel > Programs > Turn Windows features on or off, then try again: %w", err)
	}
	if vm.HypervisorReady() == nil && !reboot.Check().Needed {
		pr.logln("The Windows Hypervisor Platform is on.")
		return "", nil
	}
	pr.logln("The Windows Hypervisor Platform is on but needs a restart.")
	return "The Windows Hypervisor Platform has been turned on.\n\nWindows has to restart before the " +
		engineName + " can run. Restart this computer, then choose Check again.", nil
}

// DISM exits with 3010 when the feature was turned on and only a restart is left.
func restartRequired(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 3010
}

func (pr *Provisioner) saveEngineDiagnostics() {
	if pr.DiagnosticsDir == "" {
		return
	}
	path := filepath.Join(pr.DiagnosticsDir, "care-engine-diagnostics.txt")
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	vm.WriteDiagnostics(ctx, f)
	pr.logln("The " + engineName + "'s logs were saved to " + path + ".")
}
