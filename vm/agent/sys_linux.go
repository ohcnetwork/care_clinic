//go:build linux

package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/mdlayher/vsock"
)

func listenVsock(port uint32) (net.Listener, error) { return vsock.Listen(port, nil) }

// mountShare mounts a virtiofs share once; mounting again is a no-op so the
// host can repeat it after reconnecting.
func mountShare(tag, target string) error {
	if mounted(target) {
		return nil
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	if err := syscall.Mount(tag, target, "virtiofs", 0, ""); err != nil {
		return fmt.Errorf("mount %s at %s: %w", tag, target, err)
	}
	return nil
}

func mounted(target string) bool {
	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		// /proc/self/mounts escapes spaces as \040.
		if len(fields) > 1 && strings.ReplaceAll(fields[1], `\040`, " ") == target {
			return true
		}
	}
	return false
}

func setClock(t time.Time) error {
	tv := syscall.NsecToTimeval(t.UnixNano())
	return syscall.Settimeofday(&tv)
}

func diskUsage(p string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(p, &st); err != nil {
		return 0, 0, err
	}
	return st.Bavail * uint64(st.Bsize), st.Blocks * uint64(st.Bsize), nil
}
