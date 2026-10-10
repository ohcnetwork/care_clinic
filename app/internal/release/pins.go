package release

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/compose-spec/compose-go/v2/dotenv"
)

const EnvFile = ".env"

var (
	versionPattern   = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-dev)?$`)
	numericPattern   = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	sha256Pattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	linePattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	digestRefPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._/:-]*@sha256:[0-9a-f]{64}$`)
)

type Pins struct {
	AppVersion string

	PostgresImage string
	RedisImage    string
	MinioImage    string
	CaddyImage    string
	CorazaVersion string

	BackupImage   string
	CaddyWafImage string
	BackendImage  string
	FrontendImage string

	// LinesURL is where CI publishes the CARE release-line manifest;
	// DefaultLine is the line a new clinic installs.
	LinesURL    string
	DefaultLine string

	// Optional CI-built images for the proxy and backup sidecar. When set
	// they are pulled instead of being built on the clinic computer.
	CaddyWafPrebuilt string
	BackupPrebuilt   string

	// The CARE engine: the Linux VM that runs Docker on macOS and Windows,
	// with the docker CLI, compose plugin and (Windows) QEMU it is driven by.
	CareVMVersion             string
	CareVMBaseURL             string
	CareVMAarch64SHA256       string
	CareVMX8664SHA256         string
	DockerCLIVersion          string
	DockerCLIMacAarch64SHA256 string
	DockerCLIMacX8664SHA256   string
	DockerCLIWindowsSHA256    string
	ComposeMacAarch64SHA256   string
	ComposeMacX8664SHA256     string
	ComposeWindowsSHA256      string
	QEMUWindowsVersion        string
	QEMUWindowsSHA256         string

	DockerLinuxVersion        string
	DockerLinuxX8664SHA256    string
	DockerLinuxAarch64SHA256  string
	ComposeLinuxVersion       string
	ComposeLinuxX8664SHA256   string
	ComposeLinuxAarch64SHA256 string
}

func (p *Pins) envPinMap() []struct {
	key string
	dst *string
} {
	return []struct {
		key string
		dst *string
	}{
		{"CARE_CLINIC_VERSION", &p.AppVersion},
		{"POSTGRES_IMAGE", &p.PostgresImage},
		{"REDIS_IMAGE", &p.RedisImage},
		{"MINIO_IMAGE", &p.MinioImage},
		{"CADDY_IMAGE", &p.CaddyImage},
		{"CORAZA_VERSION", &p.CorazaVersion},
		{"BACKUP_IMAGE", &p.BackupImage},
		{"CADDY_WAF_IMAGE", &p.CaddyWafImage},
		{"BACKEND_IMAGE", &p.BackendImage},
		{"FRONTEND_IMAGE", &p.FrontendImage},
		{"CARE_LINES_URL", &p.LinesURL},
		{"CARE_DEFAULT_LINE", &p.DefaultLine},
		{"CARE_VM_VERSION", &p.CareVMVersion},
		{"CARE_VM_BASE_URL", &p.CareVMBaseURL},
		{"CARE_VM_AARCH64_SHA256", &p.CareVMAarch64SHA256},
		{"CARE_VM_X86_64_SHA256", &p.CareVMX8664SHA256},
		{"DOCKER_CLI_VERSION", &p.DockerCLIVersion},
		{"DOCKER_CLI_MACOS_AARCH64_SHA256", &p.DockerCLIMacAarch64SHA256},
		{"DOCKER_CLI_MACOS_X86_64_SHA256", &p.DockerCLIMacX8664SHA256},
		{"DOCKER_CLI_WINDOWS_X86_64_SHA256", &p.DockerCLIWindowsSHA256},
		{"COMPOSE_MACOS_AARCH64_SHA256", &p.ComposeMacAarch64SHA256},
		{"COMPOSE_MACOS_X86_64_SHA256", &p.ComposeMacX8664SHA256},
		{"COMPOSE_WINDOWS_X86_64_SHA256", &p.ComposeWindowsSHA256},
		{"QEMU_WINDOWS_VERSION", &p.QEMUWindowsVersion},
		{"QEMU_WINDOWS_SHA256", &p.QEMUWindowsSHA256},
		{"DOCKER_LINUX_VERSION", &p.DockerLinuxVersion},
		{"DOCKER_LINUX_X86_64_SHA256", &p.DockerLinuxX8664SHA256},
		{"DOCKER_LINUX_AARCH64_SHA256", &p.DockerLinuxAarch64SHA256},
		{"COMPOSE_LINUX_VERSION", &p.ComposeLinuxVersion},
		{"COMPOSE_LINUX_X86_64_SHA256", &p.ComposeLinuxX8664SHA256},
		{"COMPOSE_LINUX_AARCH64_SHA256", &p.ComposeLinuxAarch64SHA256},
	}
}

// Summary renders the pins as log lines.
func (p *Pins) Summary() []string {
	if p == nil {
		return nil
	}
	return []string{
		"pins: care     " + p.BackendImage + " + " + p.FrontendImage + " <- line " + p.DefaultLine + " (" + p.LinesURL + ")",
		"      base     " + strings.Join([]string{p.PostgresImage, p.RedisImage, p.MinioImage}, " · "),
		"      proxy    " + p.CaddyImage + " + coraza " + p.CorazaVersion,
	}
}

func Load(env []byte) (*Pins, error) {
	values, err := dotenv.Parse(bytes.NewReader(env))
	if err != nil {
		return nil, fmt.Errorf("%s is malformed: %w", EnvFile, err)
	}
	var p Pins
	var missing []string
	for _, f := range p.envPinMap() {
		v := strings.TrimSpace(values[f.key])
		if v == "" {
			missing = append(missing, f.key)
			continue
		}
		*f.dst = v
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s is missing required values: %s", EnvFile, strings.Join(missing, ", "))
	}
	if !versionPattern.MatchString(p.AppVersion) {
		return nil, fmt.Errorf("CARE_CLINIC_VERSION must be X.Y.Z or X.Y.Z-dev")
	}
	for key, version := range map[string]string{
		"CARE_VM_VERSION":       p.CareVMVersion,
		"DOCKER_CLI_VERSION":    p.DockerCLIVersion,
		"QEMU_WINDOWS_VERSION":  p.QEMUWindowsVersion,
		"DOCKER_LINUX_VERSION":  p.DockerLinuxVersion,
		"COMPOSE_LINUX_VERSION": p.ComposeLinuxVersion,
	} {
		if !numericPattern.MatchString(version) {
			return nil, fmt.Errorf("%s must be X.Y.Z", key)
		}
	}
	for key, raw := range map[string]string{"CARE_LINES_URL": p.LinesURL, "CARE_VM_BASE_URL": p.CareVMBaseURL} {
		if u, err := url.Parse(raw); err != nil || u.Scheme != "https" || u.Host == "" {
			return nil, fmt.Errorf("%s must be an https URL", key)
		}
	}
	if !linePattern.MatchString(p.DefaultLine) {
		return nil, fmt.Errorf("CARE_DEFAULT_LINE is not a valid line id")
	}
	for key, dst := range map[string]*string{
		"CADDY_WAF_PREBUILT": &p.CaddyWafPrebuilt,
		"BACKUP_PREBUILT":    &p.BackupPrebuilt,
	} {
		v := strings.TrimSpace(values[key])
		if v != "" && !digestRefPattern.MatchString(v) {
			return nil, fmt.Errorf("%s must be an image pinned by digest (name@sha256:...)", key)
		}
		*dst = v
	}
	for key, sum := range map[string]string{
		"CARE_VM_AARCH64_SHA256":           p.CareVMAarch64SHA256,
		"CARE_VM_X86_64_SHA256":            p.CareVMX8664SHA256,
		"DOCKER_CLI_MACOS_AARCH64_SHA256":  p.DockerCLIMacAarch64SHA256,
		"DOCKER_CLI_MACOS_X86_64_SHA256":   p.DockerCLIMacX8664SHA256,
		"DOCKER_CLI_WINDOWS_X86_64_SHA256": p.DockerCLIWindowsSHA256,
		"COMPOSE_MACOS_AARCH64_SHA256":     p.ComposeMacAarch64SHA256,
		"COMPOSE_MACOS_X86_64_SHA256":      p.ComposeMacX8664SHA256,
		"COMPOSE_WINDOWS_X86_64_SHA256":    p.ComposeWindowsSHA256,
		"QEMU_WINDOWS_SHA256":              p.QEMUWindowsSHA256,
		"DOCKER_LINUX_X86_64_SHA256":       p.DockerLinuxX8664SHA256,
		"DOCKER_LINUX_AARCH64_SHA256":      p.DockerLinuxAarch64SHA256,
		"COMPOSE_LINUX_X86_64_SHA256":      p.ComposeLinuxX8664SHA256,
		"COMPOSE_LINUX_AARCH64_SHA256":     p.ComposeLinuxAarch64SHA256,
	} {
		if !sha256Pattern.MatchString(sum) {
			return nil, fmt.Errorf("%s must be 64 lowercase hex characters", key)
		}
	}
	return &p, nil
}

func IsCommitRef(ref string) bool {
	if len(ref) != 40 {
		return false
	}
	_, err := hex.DecodeString(ref)
	return err == nil
}
