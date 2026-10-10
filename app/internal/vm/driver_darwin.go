//go:build darwin

package vm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"sync"
	"unsafe"

	"github.com/Code-Hex/vz/v3"
)

type appleVM struct {
	vm     *vz.VirtualMachine
	doneCh chan struct{}
	files  []*os.File
	once   sync.Once
}

func boot(o bootOptions) (machine, error) {
	m := o.manifest
	bl, err := vz.NewLinuxBootLoader(m.path(m.Kernel),
		vz.WithInitrd(m.path(m.Initrd)),
		vz.WithCommandLine(m.commandLine("hvc0")))
	if err != nil {
		return nil, err
	}
	cfg, err := vz.NewVirtualMachineConfiguration(bl, uint(o.config.CPUs), uint64(o.config.MemoryMB)<<20)
	if err != nil {
		return nil, err
	}

	var disks []vz.StorageDeviceConfiguration
	for _, d := range []struct {
		path, id string
		ro       bool
	}{
		{m.path("system.img"), "care-system", true},
		{dataDisk(), "care-data", false},
	} {
		att, err := vz.NewDiskImageStorageDeviceAttachmentWithCacheAndSync(d.path, d.ro,
			vz.DiskImageCachingModeAutomatic, vz.DiskImageSynchronizationModeFsync)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", d.path, err)
		}
		blk, err := vz.NewVirtioBlockDeviceConfiguration(att)
		if err != nil {
			return nil, err
		}
		if err := blk.SetBlockDeviceIdentifier(d.id); err != nil {
			return nil, err
		}
		disks = append(disks, blk)
	}
	cfg.SetStorageDevicesVirtualMachineConfiguration(disks)

	var shares []vz.DirectorySharingDeviceConfiguration
	for _, s := range darwinShares() {
		dir, err := vz.NewSharedDirectory(s.host, false)
		if err != nil {
			return nil, err
		}
		single, err := vz.NewSingleDirectoryShare(dir)
		if err != nil {
			return nil, err
		}
		fsd, err := vz.NewVirtioFileSystemDeviceConfiguration(s.tag)
		if err != nil {
			return nil, err
		}
		fsd.SetDirectoryShare(single)
		shares = append(shares, fsd)
	}
	cfg.SetDirectorySharingDevicesVirtualMachineConfiguration(shares)

	nat, err := vz.NewNATNetworkDeviceAttachment()
	if err != nil {
		return nil, err
	}
	nic, err := vz.NewVirtioNetworkDeviceConfiguration(nat)
	if err != nil {
		return nil, err
	}
	if mac, err := vz.NewRandomLocallyAdministeredMACAddress(); err == nil {
		nic.SetMACAddress(mac)
	}
	cfg.SetNetworkDevicesVirtualMachineConfiguration([]*vz.VirtioNetworkDeviceConfiguration{nic})

	entropy, err := vz.NewVirtioEntropyDeviceConfiguration()
	if err != nil {
		return nil, err
	}
	cfg.SetEntropyDevicesVirtualMachineConfiguration([]*vz.VirtioEntropyDeviceConfiguration{entropy})
	if balloon, err := vz.NewVirtioTraditionalMemoryBalloonDeviceConfiguration(); err == nil {
		cfg.SetMemoryBalloonDevicesVirtualMachineConfiguration([]vz.MemoryBalloonDeviceConfiguration{balloon})
	}
	sock, err := vz.NewVirtioSocketDeviceConfiguration()
	if err != nil {
		return nil, err
	}
	cfg.SetSocketDevicesVirtualMachineConfiguration([]vz.SocketDeviceConfiguration{sock})

	out, err := os.OpenFile(ConsoleLog(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	in, err := os.Open(os.DevNull)
	if err != nil {
		_ = out.Close()
		return nil, err
	}
	a := &appleVM{doneCh: make(chan struct{}), files: []*os.File{in, out}}
	serial, err := vz.NewFileHandleSerialPortAttachment(in, out)
	if err != nil {
		a.closeFiles()
		return nil, err
	}
	console, err := vz.NewVirtioConsoleDeviceSerialPortConfiguration(serial)
	if err != nil {
		a.closeFiles()
		return nil, err
	}
	cfg.SetSerialPortsVirtualMachineConfiguration([]*vz.VirtioConsoleDeviceSerialPortConfiguration{console})

	if ok, err := cfg.Validate(); !ok || err != nil {
		a.closeFiles()
		return nil, fmt.Errorf("VM configuration: %w", err)
	}
	v, err := vz.NewVirtualMachine(cfg)
	if err != nil {
		a.closeFiles()
		return nil, err
	}
	a.vm = v
	if err := v.Start(); err != nil {
		a.closeFiles()
		return nil, fmt.Errorf("%w (is the app signed with the virtualization entitlement?)", err)
	}
	go func() {
		for s := range v.StateChangedNotify() {
			if s == vz.VirtualMachineStateStopped || s == vz.VirtualMachineStateError {
				a.once.Do(func() {
					a.closeFiles()
					close(a.doneCh)
				})
				return
			}
		}
	}()
	return a, nil
}

func (a *appleVM) closeFiles() {
	for _, f := range a.files {
		_ = f.Close()
	}
}

func (a *appleVM) done() <-chan struct{} { return a.doneCh }

func (a *appleVM) kill() error {
	if a.vm.CanStop() {
		return a.vm.Stop()
	}
	return nil
}

func (a *appleVM) dial(ctx context.Context, port uint32) (net.Conn, error) {
	devs := a.vm.SocketDevices()
	if len(devs) == 0 {
		return nil, errors.New("the VM has no socket device")
	}
	type result struct {
		c   *vz.VirtioSocketConnection
		err error
	}
	ch := make(chan result, 1)
	go func() {
		c, err := devs[0].Connect(port)
		ch <- result{c, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		return halfCloser(r.c), nil
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.c != nil {
				_ = r.c.Close()
			}
		}()
		return nil, ctx.Err()
	}
}

// halfCloser exposes CloseWrite on the socket vz wraps, so a finished
// request direction can be signalled without dropping the response.
func halfCloser(c *vz.VirtioSocketConnection) net.Conn {
	f := reflect.ValueOf(c).Elem().FieldByName("rawConn")
	if !f.IsValid() || f.Type() != reflect.TypeOf((*net.Conn)(nil)).Elem() {
		return c
	}
	raw := *(*net.Conn)(unsafe.Pointer(f.UnsafeAddr()))
	if _, ok := raw.(closeWriter); !ok {
		return c
	}
	return raw
}
