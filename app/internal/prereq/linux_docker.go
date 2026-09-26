package prereq

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
)

const (
	dockerLinuxBinDir    = "/usr/local/bin"
	dockerLinuxCompose   = "/usr/local/lib/docker/cli-plugins/docker-compose"
	dockerLinuxUnitPath  = "/etc/systemd/system/docker.service"
	dockerLinuxReadyWait = 30 * time.Second
)

const dockerLinuxUnit = `[Unit]
Description=Docker Application Container Engine (installed by CARE Desktop)
Documentation=https://docs.docker.com
After=network-online.target firewalld.service time-set.target
Wants=network-online.target

[Service]
Type=notify
ExecStart=` + dockerLinuxBinDir + `/dockerd
ExecReload=/bin/kill -s HUP $MAINPID
TimeoutStartSec=0
Restart=always
RestartSec=2
LimitNPROC=infinity
LimitCORE=infinity
TasksMax=infinity
Delegate=yes
KillMode=process
OOMScoreAdjust=-500

[Install]
WantedBy=multi-user.target
`

func linuxInstallCommand(pm, pkg string) string {
	switch pm {
	case "apt":
		return "apt-get update && apt-get install -y " + pkg
	case "dnf":
		return "dnf install -y " + pkg
	case "zypper":
		return "zypper --non-interactive install " + pkg
	case "pacman":
		return "pacman -Sy --noconfirm " + pkg
	}
	return ""
}

func (pr *Provisioner) installDockerLinux() error {
	engine, compose, err := dockerLinuxDownloads(pr.pins, runtime.GOARCH)
	if err != nil {
		return err
	}
	if pr.dockerDaemonUp() {
		return pr.installComposeLinux(compose)
	}
	if !hasCommand("systemctl") {
		return fmt.Errorf("this system does not use systemd, so CARE cannot run Docker as a service - install Docker from %s",
			dockerEnginePage)
	}
	var steps []string
	if !hasCommand("iptables") {
		pm := linuxPackageManager()
		if pm == "" {
			return errors.New("Docker needs iptables, which is not installed, and no supported package manager " +
				"was found - install iptables, then try again")
		}
		steps = append(steps, linuxInstallCommand(pm, "iptables"))
	}

	dir, err := os.MkdirTemp("", "care-docker-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	unit := filepath.Join(dir, "docker.service")
	if err := os.WriteFile(unit, []byte(dockerLinuxUnit), 0o644); err != nil {
		return err
	}
	archive, err := pr.download(engine)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(archive) }()
	plugin, err := pr.download(compose)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(plugin) }()

	extracted := filepath.Join(dir, "engine")
	steps = append(steps,
		"mkdir "+elevate.ShQuote(extracted),
		"tar -xzf "+elevate.ShQuote(archive)+" -C "+elevate.ShQuote(extracted),
		"install -m 0755 "+elevate.ShQuote(extracted)+"/docker/* "+dockerLinuxBinDir+"/",
		"install -D -m 0755 "+elevate.ShQuote(plugin)+" "+dockerLinuxCompose,
		"install -m 0644 "+elevate.ShQuote(unit)+" "+dockerLinuxUnitPath,
		"groupadd -f docker",
		"usermod -aG docker "+elevate.ShQuote(currentUsername()),
		"systemctl daemon-reload",
		"systemctl enable --now docker",
	)
	sh := "trap " + elevate.ShQuote("rm -rf "+elevate.ShQuote(extracted)) + " EXIT; " + strings.Join(steps, " && ")
	pr.logln("Installing Docker Engine " + pr.pins.DockerLinuxVersion + " and Compose " +
		pr.pins.ComposeLinuxVersion + ". You'll be asked for your password...")
	if err := elevate.Run(sh, true); err != nil {
		return fmt.Errorf("could not install Docker: %w", err)
	}
	pr.logln("Docker installed. If the check below still fails, log out and back in " +
		"so this account picks up its new 'docker' group membership.")
	return pr.waitForDocker(dockerLinuxReadyWait)
}

func (pr *Provisioner) installComposeLinux(compose Download) error {
	plugin, err := pr.download(compose)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(plugin) }()
	pr.logln("Installing the Docker Compose plugin " + pr.pins.ComposeLinuxVersion + ". You'll be asked for your password...")
	if err := elevate.Run("install -D -m 0755 "+elevate.ShQuote(plugin)+" "+dockerLinuxCompose, true); err != nil {
		return fmt.Errorf("could not install the Docker Compose plugin: %w", err)
	}
	pr.logln("Docker Compose plugin installed.")
	return pr.waitForDocker(dockerLinuxReadyWait)
}
