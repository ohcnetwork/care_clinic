package prereq

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/release"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
	"github.com/ohcnetwork/care_desktop/app/internal/vm"
)

type Provisioner struct {
	Log func(string)
	// DiagnosticsDir receives the CARE engine's logs when it fails to start.
	DiagnosticsDir string
	Progress       func(DownloadProgress)
	run            proc.Runner
	pins           *release.Pins
}

func NewProvisioner(run proc.Runner, pins *release.Pins, log func(string)) *Provisioner {
	return &Provisioner{Log: log, run: run, pins: pins}
}

func (pr *Provisioner) logln(s string) {
	if pr.Log != nil {
		pr.Log(s)
	}
}

const (
	engineHelpURL    = "https://github.com/ohcnetwork/care_clinic/blob/main/docs/vm.md"
	dockerEnginePage = "https://docs.docker.com/engine/install/"

	dockerReadyTimeout = 8 * time.Minute
)

func dockerHelpURL() string {
	if runtime.GOOS == "linux" {
		return dockerEnginePage
	}
	return engineHelpURL
}

type ToolAction string

const (
	ActionNone    ToolAction = ""
	ActionInstall ToolAction = "install"
	ActionOpen    ToolAction = "open"
	ActionManual  ToolAction = "manual"
)

type ToolPlan struct {
	Action          ToolAction `json:"action"`
	Label           string     `json:"label"`
	Detail          string     `json:"detail"`
	URL             string     `json:"url"`
	DownloadPreview bool       `json:"download_preview"`
}

func (pr *Provisioner) DockerPlan() ToolPlan {
	if DockerCheck(pr.run).OK {
		return ToolPlan{URL: dockerHelpURL()}
	}
	if vm.Supported() {
		return pr.enginePlan()
	}
	if runtime.GOOS == "linux" && !pr.dockerDaemonUp() && hasCommand("docker") && hasCommand("systemctl") {
		return ToolPlan{
			Action: ActionOpen, Label: "Start Docker", URL: dockerHelpURL(),
			Detail: "Starts Docker and waits for it to be ready.",
		}
	}
	return pr.dockerInstallPlan()
}

func (pr *Provisioner) dockerInstallPlan() ToolPlan {
	p := ToolPlan{Action: ActionInstall, Label: "Install Docker", URL: dockerHelpURL()}
	switch runtime.GOOS {
	case "linux":
		if _, ok := linuxArch(runtime.GOARCH); !ok || !hasCommand("systemctl") {
			p.Action, p.Label = ActionManual, "Get Docker"
			p.Detail = "Install Docker Engine and the Compose plugin for this system."
			return p
		}
		if pr.dockerDaemonUp() {
			p.Detail = "Downloads the Docker Compose plugin and installs it. You'll be asked for your password."
			return p
		}
		p.Detail = "Downloads Docker Engine and the Compose plugin, installs them, and starts Docker. " +
			"You'll be asked for your password. Keep this computer connected to the internet."
	default:
		p.Action, p.Label = ActionManual, "Get Docker"
		p.Detail = "Install Docker for this system."
	}
	return p
}

func dockerName() string {
	if vm.Supported() {
		return engineName
	}
	return "Docker"
}

func (pr *Provisioner) InstallDocker() (string, error) {
	switch {
	case vm.Supported():
		if err := pr.installEngine(); err != nil {
			return "", err
		}
		return "", nil
	case runtime.GOOS == "linux":
		if err := pr.installDockerLinux(); err != nil {
			return "", err
		}
		return "Docker is installed.\n\nIf the check still fails, log out and back in so your user " +
			"picks up the docker group, then choose Check again.", nil
	default:
		return "", fmt.Errorf("installing Docker isn't supported on %s - install it from %s", runtime.GOOS, dockerHelpURL())
	}
}

func (pr *Provisioner) OpenDocker() error {
	if vm.Supported() {
		return pr.startEngine()
	}
	pr.logln("Starting Docker...")
	if err := elevate.Run("systemctl start docker", true); err != nil {
		return fmt.Errorf("could not start the Docker service: %w", err)
	}
	return pr.waitForDocker(dockerReadyTimeout)
}

func (pr *Provisioner) waitForDocker(limit time.Duration) error {
	return waitForDockerReady(limit, func() bool { return DockerCheck(pr.run).OK }, pr.logln)
}

func waitForDockerReady(limit time.Duration, ready func() bool, log func(string)) error {
	deadline := time.Now().Add(limit)
	for {
		if ready() {
			log("Docker is ready.")
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("%s is taking longer than usual to start; it may still be starting. "+
				"Wait a minute, then run the check again", dockerName())
		}
		time.Sleep(min(3*time.Second, time.Until(deadline)))
	}
}

func (pr *Provisioner) dockerDaemonUp() bool {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := proc.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}")
	cmd.Env = pr.run.Env
	return cmd.Run() == nil
}

func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func linuxPackageManager() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	for _, pm := range []string{"apt", "dnf", "zypper", "pacman"} {
		lookup := pm
		if pm == "apt" {
			lookup = "apt-get"
		}
		if hasCommand(lookup) {
			return pm
		}
	}
	return ""
}

func currentUsername() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

func (pr *Provisioner) runElevated(exe string, args ...string) error {
	if runtime.GOOS == "windows" {
		ps := "$p = Start-Process " + elevate.PSQuote(exe) + " -Wait -PassThru -Verb RunAs -WindowStyle Hidden"
		if len(args) > 0 {
			quoted := make([]string, 0, len(args))
			for _, a := range args {
				quoted = append(quoted, elevate.PSQuote(a))
			}
			ps += " -ArgumentList " + strings.Join(quoted, ",")
		}
		ps += "; exit $p.ExitCode"
		return proc.Command("powershell", "-NoProfile", "-Command", ps).Run()
	}
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, elevate.ShQuote(exe))
	for _, a := range args {
		parts = append(parts, elevate.ShQuote(a))
	}
	return elevate.Run(strings.Join(parts, " "), true)
}

const downloadHeaderTimeout = 30 * time.Second

var downloadStallTimeout = 2 * time.Minute

func downloadClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = downloadHeaderTimeout
	return &http.Client{Transport: tr}
}

type DownloadProgress struct {
	Name  string `json:"name"`
	Phase string `json:"phase"`
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}

func (pr *Provisioner) download(d Download) (path string, resultErr error) {
	name := d.Name
	var done, total int64
	report := func(phase string) {
		if pr.Progress != nil {
			pr.Progress(DownloadProgress{Name: name, Phase: phase, Done: done, Total: total})
		}
	}
	report("connecting")
	defer func() {
		if resultErr != nil {
			report("failed")
		}
	}()
	pr.logln("Downloading " + name + " from " + hostOf(d.URL) + "...")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stall := time.AfterFunc(downloadStallTimeout, cancel)
	defer stall.Stop()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL, nil)
	if err != nil {
		return "", err
	}
	client := downloadClient()
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not download %s: %w", name, downloadError(err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("could not download %s: the server said %s", name, resp.Status)
	}
	total = max(resp.ContentLength, 0)
	report("downloading")

	f, err := os.CreateTemp("", "care-*-"+name)
	if err != nil {
		return "", err
	}
	path = f.Name()
	sum := sha256.New()
	done, err = io.Copy(io.MultiWriter(f, sum), &progressReader{
		r:     resp.Body,
		total: resp.ContentLength,
		log:   pr.logln,
		name:  name,
		alive: func() { stall.Reset(downloadStallTimeout) },
		progress: func(read int64) {
			done = read
			report("downloading")
		},
	})
	closeErr := f.Close()
	if err != nil {
		_ = os.Remove(path)
		if ctx.Err() != nil {
			return "", downloadError(fmt.Errorf("the download of %s stopped making progress for %s - "+
				"check this computer's internet connection and try again: %w", name, downloadStallTimeout, ctx.Err()))
		}
		return "", fmt.Errorf("could not download %s: %w", name, downloadError(err))
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return "", closeErr
	}
	report("verifying")
	if got := hex.EncodeToString(sum.Sum(nil)); got != d.SHA256 {
		_ = os.Remove(path)
		return "", fmt.Errorf("the downloaded %s is not the file this version of CARE was tested with "+
			"(expected SHA-256 %s, got %s), so it was not installed - try again, and report this if it keeps happening",
			name, d.SHA256, got)
	}
	pr.logln("Downloaded " + name + " and verified its checksum.")
	report("complete")
	return path, nil
}

type progressReader struct {
	r          io.Reader
	total      int64
	read       int64
	lastStep   int64
	log        func(string)
	name       string
	alive      func()
	progress   func(int64)
	lastReport time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.read += int64(n)
	if n > 0 && p.alive != nil {
		p.alive()
	}
	if p.total > 0 {
		if step := p.read * 10 / p.total; step > p.lastStep {
			p.lastStep = step
			p.log(fmt.Sprintf("  %s: %d%%", p.name, step*10))
		}
	}
	if n > 0 && p.progress != nil && (p.lastReport.IsZero() || time.Since(p.lastReport) >= 200*time.Millisecond) {
		p.lastReport = time.Now()
		p.progress(p.read)
	}
	return n, err
}

func hostOf(rawURL string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(rawURL, "https://"), "http://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		return s[:i]
	}
	return s
}
