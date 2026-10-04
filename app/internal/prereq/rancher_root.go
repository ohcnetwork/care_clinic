package prereq

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

const (
	rancherOptDir      = "/opt/rancher-desktop"
	rancherSudoersPath = "/private/etc/sudoers.d/zzzzz-rancher-desktop-lima"
	rancherOldSudoers  = "/private/etc/sudoers.d/rancher-desktop-lima"
	dockerSockLink     = "/var/run/docker.sock"
	dockerSockDaemon   = "/Library/LaunchDaemons/org.ohcnetwork.care-clinic.docker-socket.plist"
	dockerSockLabel    = "org.ohcnetwork.care-clinic.docker-socket"
)

type rootSetup struct {
	cmds  []string
	temps []string
}

func (r *rootSetup) cleanup() {
	for _, t := range r.temps {
		_ = os.Remove(t)
	}
}

func (r *rootSetup) stage(pattern, body string) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	r.temps = append(r.temps, f.Name())
	if _, err := f.WriteString(body); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), os.Chmod(f.Name(), 0o644)
}

func rancherRootSetup(all bool) (*rootSetup, error) {
	r := &rootSetup{}
	home, err := os.UserHomeDir()
	if err != nil {
		return r, err
	}

	vmnet := filepath.Join(rancherAppMac, "Contents", "Resources", "resources", "darwin", "lima", "socket_vmnet")
	if all || !treeMatches(vmnet, rancherOptDir) {
		r.cmds = append(r.cmds, "mkdir -p "+rancherOptDir+
			" && ditto "+elevate.ShQuote(vmnet)+" "+rancherOptDir+
			" && chown -R root:wheel "+rancherOptDir+
			" && chmod -R go-w "+rancherOptDir)
	}

	ifaces, err := darwinHostInterfaces()
	if err != nil {
		return r, err
	}
	sudoers := rancherSudoers(ifaces)
	if current, err := os.ReadFile(rancherSudoersPath); all || err != nil || string(current) != sudoers {
		tmp, err := r.stage("care-rd-sudoers-*", sudoers)
		if err != nil {
			return r, err
		}
		r.cmds = append(r.cmds, "/usr/sbin/visudo -cf "+elevate.ShQuote(tmp)+
			" && mkdir -p /private/etc/sudoers.d"+
			" && install -m 0644 -o root -g wheel "+elevate.ShQuote(tmp)+" "+rancherSudoersPath)
	}
	if _, err := os.Lstat(rancherOldSudoers); err == nil {
		r.cmds = append(r.cmds, "rm -f "+rancherOldSudoers)
	}
	if stale := staleVMNetFiles(vmnetRunDir, processAlive); len(stale) > 0 {
		r.cmds = append(r.cmds, "rm -f "+shQuoteAll(stale))
	}

	sock := filepath.Join(home, ".rd", "docker.sock")
	if target, err := os.Readlink(dockerSockLink); all || err != nil || target != sock {
		r.cmds = append(r.cmds, "ln -sf "+elevate.ShQuote(sock)+" "+dockerSockLink)
	}

	plist := dockerSockPlist(sock)
	if current, err := os.ReadFile(dockerSockDaemon); all || err != nil || string(current) != plist {
		tmp, err := r.stage("care-docker-socket-*.plist", plist)
		if err != nil {
			return r, err
		}
		r.cmds = append(r.cmds, "install -m 0644 -o root -g wheel "+elevate.ShQuote(tmp)+" "+dockerSockDaemon)
	}
	return r, nil
}

func (pr *Provisioner) ensureRancherRoot() error {
	setup, err := rancherRootSetup(false)
	defer setup.cleanup()
	if err != nil {
		return fmt.Errorf("could not check Rancher Desktop's administrator setup: %w", err)
	}
	if len(setup.cmds) == 0 {
		return nil
	}
	pr.logln("Rancher Desktop needs administrator approval once to forward ports 80 and 443. macOS will ask for your password...")
	if err := elevate.Run(strings.Join(setup.cmds, " && "), true); err != nil {
		return fmt.Errorf("Rancher Desktop needs administrator approval to forward ports 80 and 443: %w", err)
	}
	return nil
}

func treeMatches(src, dst string) bool {
	matched := true
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		want, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil || !bytes.Equal(want, got) {
			matched = false
			return fs.SkipAll
		}
		return nil
	})
	return err == nil && matched
}

func darwinHostInterfaces() ([]string, error) {
	out, err := proc.Command("/usr/sbin/system_profiler", "SPNetworkDataType", "-json", "-detailLevel", "basic").Output()
	if err != nil {
		return nil, fmt.Errorf("could not list network interfaces: %w", err)
	}
	return parseHostInterfaces(out)
}

func parseHostInterfaces(raw []byte) ([]string, error) {
	var report struct {
		SPNetworkDataType []struct {
			Interface string `json:"interface"`
		}
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, fmt.Errorf("could not read network interfaces: %w", err)
	}
	seen := map[string]bool{}
	var ifaces []string
	for _, n := range report.SPNetworkDataType {
		if n.Interface != "" && !seen[n.Interface] {
			seen[n.Interface] = true
			ifaces = append(ifaces, n.Interface)
		}
	}
	sort.Strings(ifaces)
	return ifaces, nil
}

func rancherSudoers(ifaces []string) string {
	block := func(name, args string) string {
		return `# Manage "` + name + "\" network daemons\n\n" +
			"%everyone ALL=(root:wheel) NOPASSWD:NOSETENV: \\\n" +
			"    /opt/rancher-desktop/bin/socket_vmnet --pidfile=/private/var/run/" + name + "_socket_vmnet.pid --socket-group=everyone " +
			args + " /private/var/run/socket_vmnet." + name + ", \\\n" +
			"    /usr/bin/pkill -F /private/var/run/" + name + "_socket_vmnet.pid\n"
	}
	var b strings.Builder
	b.WriteString("%everyone ALL=(root:wheel) NOPASSWD:NOSETENV: /bin/mkdir -m 775 -p /private/var/run\n\n")
	b.WriteString(block("host", "--vmnet-mode=host --vmnet-gateway=192.168.206.1 --vmnet-dhcp-end=192.168.206.254 --vmnet-mask=255.255.255.0"))
	b.WriteString("\n")
	for _, iface := range ifaces {
		b.WriteString(block("rancher-desktop-bridged_"+iface, "--vmnet-mode=bridged --vmnet-interface="+iface))
		b.WriteString("\n")
	}
	b.WriteString(block("rancher-desktop-shared", "--vmnet-mode=shared --vmnet-gateway=192.168.205.1 --vmnet-dhcp-end=192.168.205.254 --vmnet-mask=255.255.255.0"))
	return b.String()
}

func dockerSockPlist(sock string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>` + dockerSockLabel + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>/bin/ln</string>
		<string>-sf</string>
		<string>` + xmlEscape(sock) + `</string>
		<string>` + dockerSockLink + `</string>
	</array>
	<key>RunAtLoad</key><true/>
</dict>
</plist>
`
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
