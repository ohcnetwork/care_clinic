package prereq

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"strings"

	"github.com/ohcnetwork/care_desktop/app/internal/release"
)

const (
	rancherReleases    = "https://github.com/rancher-sandbox/rancher-desktop/releases/download/v"
	gitWindowsReleases = "https://github.com/git-for-windows/git/releases/download/v"
	dockerLinuxStatic  = "https://download.docker.com/linux/static/stable/"
	composeReleases    = "https://github.com/docker/compose/releases/download/v"
)

type Download struct {
	Name   string
	URL    string
	SHA256 string
}

type DownloadInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

func (pr *Provisioner) RancherDownloadInfo() (DownloadInfo, error) {
	d, err := rancherDownload(pr.pins, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return DownloadInfo{}, err
	}
	return inspectDownload(d)
}

// Only retrieve headers: the installer body is not requested before consent.
func inspectDownload(d Download) (DownloadInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), downloadHeaderTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, d.URL, nil)
	if err != nil {
		return DownloadInfo{}, err
	}
	client := downloadClient()
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return DownloadInfo{}, fmt.Errorf("could not check the download size: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return DownloadInfo{}, fmt.Errorf("could not check the download size: the server said %s", resp.Status)
	}
	if resp.ContentLength <= 0 {
		return DownloadInfo{}, fmt.Errorf("the server did not provide the size of %s; try again before downloading", d.Name)
	}
	return DownloadInfo{Name: d.Name, Size: resp.ContentLength}, nil
}

func rancherDownload(p *release.Pins, goos, goarch string) (Download, error) {
	var name, sum string
	switch {
	case goos == "darwin" && goarch == "arm64":
		name, sum = "Rancher.Desktop-"+p.RancherVersion+".aarch64.dmg", p.RancherMacArm64SHA256
	case goos == "darwin" && goarch == "amd64":
		name, sum = "Rancher.Desktop-"+p.RancherVersion+".x86_64.dmg", p.RancherMacX8664SHA256
	case goos == "windows" && goarch == "amd64":
		name, sum = "Rancher.Desktop.Setup."+p.RancherVersion+".msi", p.RancherWindowsSHA256
	default:
		return Download{}, fmt.Errorf("CARE does not install Rancher Desktop on %s/%s - install it yourself from %s",
			goos, goarch, rancherPageURL)
	}
	return Download{Name: name, URL: rancherReleases + p.RancherVersion + "/" + name, SHA256: sum}, nil
}

func gitWindowsDownload(p *release.Pins) Download {
	base, build, _ := strings.Cut(p.GitWindowsVersion, ".windows.")
	name := "Git-" + base
	if build != "1" {
		name += "." + build
	}
	name += "-64-bit.exe"
	return Download{Name: name, URL: gitWindowsReleases + p.GitWindowsVersion + "/" + name, SHA256: p.GitWindowsSHA256}
}

func linuxArch(goarch string) (string, bool) {
	switch goarch {
	case "amd64":
		return "x86_64", true
	case "arm64":
		return "aarch64", true
	}
	return "", false
}

func dockerLinuxDownloads(p *release.Pins, goarch string) (engine, compose Download, err error) {
	arch, ok := linuxArch(goarch)
	if !ok {
		return Download{}, Download{}, fmt.Errorf("CARE does not install Docker on linux/%s - install it yourself from %s",
			goarch, dockerEnginePage)
	}
	engineSum, composeSum := p.DockerLinuxX8664SHA256, p.ComposeLinuxX8664SHA256
	if arch == "aarch64" {
		engineSum, composeSum = p.DockerLinuxAarch64SHA256, p.ComposeLinuxAarch64SHA256
	}
	engineName := "docker-" + p.DockerLinuxVersion + ".tgz"
	composeName := "docker-compose-linux-" + arch
	engine = Download{Name: engineName, URL: dockerLinuxStatic + arch + "/" + engineName, SHA256: engineSum}
	compose = Download{Name: composeName, URL: composeReleases + p.ComposeLinuxVersion + "/" + composeName, SHA256: composeSum}
	return engine, compose, nil
}
