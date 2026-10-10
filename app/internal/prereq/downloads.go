package prereq

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/ohcnetwork/care_desktop/app/internal/release"
)

func downloadError(err error) error {
	var connection *net.OpError
	var dns *net.DNSError
	var network net.Error
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, context.Canceled) ||
		errors.As(err, &connection) || errors.As(err, &dns) || errors.As(err, &network) && network.Timeout() {
		// The native bridge sends errors as text; retain a stable marker and the original cause.
		return fmt.Errorf("download connection interrupted: %w", err)
	}
	return err
}

const (
	dockerLinuxStatic = "https://download.docker.com/linux/static/stable/"
	composeReleases   = "https://github.com/docker/compose/releases/download/v"
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
		return DownloadInfo{}, fmt.Errorf("could not check the download size: %w", downloadError(err))
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
