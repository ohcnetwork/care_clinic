//go:build windows

package vm

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type qemuVM struct {
	cmd    *exec.Cmd
	doneCh chan struct{}
	tls    *tls.Config
}

// HypervisorReady reports whether the Windows Hypervisor Platform (WHPX) is
// turned on and usable.
func HypervisorReady() error {
	dll := windows.NewLazySystemDLL("WinHvPlatform.dll")
	if err := dll.Load(); err != nil {
		return errors.New("Windows Hypervisor Platform is not turned on")
	}
	var present uint32
	r, _, _ := dll.NewProc("WHvGetCapability").Call(0, uintptr(unsafe.Pointer(&present)), unsafe.Sizeof(present), 0)
	if r != 0 || present == 0 {
		return errors.New("Windows Hypervisor Platform is turned on but not usable; turn on virtualization in the computer's firmware (BIOS/UEFI) settings and restart")
	}
	return nil
}

// The helper is the only owner of QEMU: if the helper dies, so does the VM,
// rather than leaving a VM nobody can reach.
var vmJob windows.Handle

func boot(o bootOptions) (machine, error) {
	if err := HypervisorReady(); err != nil {
		return nil, err
	}
	m := o.manifest
	creds, err := ensureCerts(certDir())
	if err != nil {
		return nil, err
	}
	tcfg, err := clientTLS()
	if err != nil {
		return nil, err
	}
	args := qemuArgs(m, o.config, creds)
	cmd := exec.Command(qemuBinary(), args...)
	if err := intoJob(cmd); err != nil {
		return nil, err
	}
	out, err := os.OpenFile(ConsoleLog(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		_ = out.Close()
		return nil, err
	}
	v := &qemuVM{cmd: cmd, doneCh: make(chan struct{}), tls: tcfg}
	go func() {
		_ = cmd.Wait()
		_ = out.Close()
		close(v.doneCh)
	}()
	return v, nil
}

func qemuArgs(m Manifest, c Config, creds string) []string {
	fwd := strings.Join([]string{
		"user,id=net0",
		"hostfwd=tcp:0.0.0.0:80-:80",
		"hostfwd=tcp:0.0.0.0:443-:443",
		"hostfwd=tcp:127.0.0.1:" + strconv.Itoa(hostTCPDocker) + "-:" + strconv.Itoa(guestTCPDocker),
		"hostfwd=tcp:127.0.0.1:" + strconv.Itoa(hostTCPControl) + "-:" + strconv.Itoa(guestTCPControl),
	}, ",")
	return []string{
		"-machine", "q35", "-accel", "whpx,kernel-irqchip=off", "-cpu", "max",
		"-m", strconv.Itoa(c.MemoryMB), "-smp", strconv.Itoa(c.CPUs),
		"-display", "none", "-monitor", "none", "-serial", "stdio", "-no-reboot",
		"-L", filepath.Join(qemuDir(), "share"),
		"-kernel", m.path(m.Kernel), "-initrd", m.path(m.Initrd),
		"-append", m.commandLine("ttyS0"),
		"-fw_cfg", "name=opt/care/tls,file=" + qemuEscape(creds),
		"-drive", "file=" + qemuEscape(m.path("system.img")) + ",format=raw,if=none,id=care-system,readonly=on",
		"-device", "virtio-blk-pci,drive=care-system,serial=care-system",
		"-drive", "file=" + qemuEscape(dataDisk()) + ",format=raw,if=none,id=care-data,discard=unmap,cache=writeback",
		"-device", "virtio-blk-pci,drive=care-data,serial=care-data",
		"-device", "virtio-net-pci,netdev=net0", "-netdev", fwd,
		"-device", "virtio-rng-pci",
	}
}

func clientTLS() (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(filepath.Join(certDir(), "cert.pem"), filepath.Join(certDir(), "key.pem"))
	if err != nil {
		return nil, err
	}
	ca, err := os.ReadFile(filepath.Join(certDir(), "ca.pem"))
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, errors.New("engine certificate is unreadable")
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}, RootCAs: pool, ServerName: "care-engine"}, nil
}

func intoJob(cmd *exec.Cmd) error {
	if vmJob == 0 {
		job, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return err
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			_ = windows.CloseHandle(job)
			return err
		}
		if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
			_ = windows.CloseHandle(job)
			return err
		}
		vmJob = job
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	return nil
}

func (v *qemuVM) done() <-chan struct{} { return v.doneCh }

var killOnce sync.Once

func (v *qemuVM) kill() error {
	var err error
	killOnce.Do(func() { err = v.cmd.Process.Kill() })
	return err
}

func (v *qemuVM) dial(ctx context.Context, port uint32) (net.Conn, error) {
	var hp int
	switch port {
	case vsockControl:
		hp = hostTCPControl
	case vsockDocker:
		hp = hostTCPDocker
	default:
		return nil, fmt.Errorf("guest port %d is not forwarded", port)
	}
	return (&tls.Dialer{Config: v.tls}).DialContext(ctx, "tcp", "127.0.0.1:"+strconv.Itoa(hp))
}

// makeSparse stops NTFS from reserving the whole data disk up front.
func makeSparse(p string) {
	h, err := windows.Open(p, windows.O_RDWR, 0)
	if err != nil {
		return
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var n uint32
	_ = windows.DeviceIoControl(h, windows.FSCTL_SET_SPARSE, nil, 0, nil, 0, &n, nil)
}

// Guest TCP ports for the Windows endpoint (QEMU hostfwd targets); shared
// with vm/agent/main.go.
const (
	guestTCPDocker  = 2376
	guestTCPControl = 2377
)
