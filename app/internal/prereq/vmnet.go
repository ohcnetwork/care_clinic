package prereq

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
)

const (
	vmnetRunDir     = "/private/var/run"
	vmnetSockPrefix = "socket_vmnet."
	vmnetPidSuffix  = "_socket_vmnet.pid"
)

type vmnetDaemon struct {
	name string
	sock string
	pid  string
	live bool
}

func rancherVMNetName(name string) bool {
	return name == "host" || strings.HasPrefix(name, "rancher-desktop-")
}

func rancherVMNetDaemons(dir string, alive func(int) bool) []vmnetDaemon {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	byName := map[string]*vmnetDaemon{}
	get := func(name string) *vmnetDaemon {
		if byName[name] == nil {
			byName[name] = &vmnetDaemon{name: name}
		}
		return byName[name]
	}
	for _, e := range entries {
		n := e.Name()
		switch {
		case strings.HasPrefix(n, vmnetSockPrefix) && rancherVMNetName(strings.TrimPrefix(n, vmnetSockPrefix)):
			get(strings.TrimPrefix(n, vmnetSockPrefix)).sock = filepath.Join(dir, n)
		case strings.HasSuffix(n, vmnetPidSuffix) && rancherVMNetName(strings.TrimSuffix(n, vmnetPidSuffix)):
			get(strings.TrimSuffix(n, vmnetPidSuffix)).pid = filepath.Join(dir, n)
		}
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	daemons := make([]vmnetDaemon, 0, len(names))
	for _, n := range names {
		d := byName[n]
		d.live = d.pid != "" && pidFileAlive(d.pid, alive)
		daemons = append(daemons, *d)
	}
	return daemons
}

func pidFileAlive(path string, alive func(int) bool) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return !os.IsNotExist(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false
	}
	return alive(pid)
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return !errors.Is(p.Signal(syscall.Signal(0)), os.ErrProcessDone)
}

func (d vmnetDaemon) files() []string {
	var out []string
	for _, f := range []string{d.sock, d.pid} {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

func vmnetPaths(dir, name string) []string {
	return []string{filepath.Join(dir, vmnetSockPrefix+name), filepath.Join(dir, name+vmnetPidSuffix)}
}

func vmnetSudoName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

func staleVMNetSudoCmds(dir string, alive func(int) bool) [][]string {
	var cmds [][]string
	for _, d := range rancherVMNetDaemons(dir, alive) {
		if !d.live && vmnetSudoName(d.name) {
			cmds = append(cmds, append([]string{"-n", "/bin/rm", "-f"}, vmnetPaths(dir, d.name)...))
		}
	}
	return cmds
}

func staleVMNetFiles(dir string, alive func(int) bool) []string {
	var out []string
	for _, d := range rancherVMNetDaemons(dir, alive) {
		if !d.live {
			out = append(out, d.files()...)
		}
	}
	return out
}

func rancherVMNetRemoveCmds(dir string, alive func(int) bool) []string {
	var cmds, files []string
	for _, d := range rancherVMNetDaemons(dir, alive) {
		if d.live {
			cmds = append(cmds, "pkill -F "+elevate.ShQuote(d.pid)+" socket_vmnet 2>/dev/null")
		}
		files = append(files, d.files()...)
	}
	if len(files) > 0 {
		cmds = append(cmds, "rm -f "+shQuoteAll(files))
	}
	return cmds
}

func shQuoteAll(paths []string) string {
	q := make([]string, len(paths))
	for i, p := range paths {
		q[i] = elevate.ShQuote(p)
	}
	return strings.Join(q, " ")
}
