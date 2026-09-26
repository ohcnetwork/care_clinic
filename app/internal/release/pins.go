package release

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/compose-spec/compose-go/v2/dotenv"
)

const EnvFile = ".env"

var (
	versionPattern    = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-dev)?$`)
	numericPattern    = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	gitWindowsPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+\.windows\.[1-9][0-9]*$`)
	sha256Pattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
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

	BeRepo string
	FeRepo string
	BeRef  string
	FeRef  string

	RancherVersion            string
	RancherMacArm64SHA256     string
	RancherMacX8664SHA256     string
	RancherWindowsSHA256      string
	GitWindowsVersion         string
	GitWindowsSHA256          string
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
		{"CARE_DESKTOP_VERSION", &p.AppVersion},
		{"POSTGRES_IMAGE", &p.PostgresImage},
		{"REDIS_IMAGE", &p.RedisImage},
		{"MINIO_IMAGE", &p.MinioImage},
		{"CADDY_IMAGE", &p.CaddyImage},
		{"CORAZA_VERSION", &p.CorazaVersion},
		{"BACKUP_IMAGE", &p.BackupImage},
		{"CADDY_WAF_IMAGE", &p.CaddyWafImage},
		{"BACKEND_IMAGE", &p.BackendImage},
		{"FRONTEND_IMAGE", &p.FrontendImage},
		{"CARE_BE_REPO", &p.BeRepo},
		{"CARE_FE_REPO", &p.FeRepo},
		{"CARE_BE_REF", &p.BeRef},
		{"CARE_FE_REF", &p.FeRef},
		{"RANCHER_VERSION", &p.RancherVersion},
		{"RANCHER_MACOS_ARM64_SHA256", &p.RancherMacArm64SHA256},
		{"RANCHER_MACOS_X86_64_SHA256", &p.RancherMacX8664SHA256},
		{"RANCHER_WINDOWS_SHA256", &p.RancherWindowsSHA256},
		{"GIT_WINDOWS_VERSION", &p.GitWindowsVersion},
		{"GIT_WINDOWS_SHA256", &p.GitWindowsSHA256},
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
		"pins: backend  " + p.BackendImage + " <- " + p.BeRepo + "@" + p.BeRef,
		"      frontend " + p.FrontendImage + " <- " + p.FeRepo + "@" + p.FeRef,
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
		return nil, fmt.Errorf("CARE_DESKTOP_VERSION must be X.Y.Z or X.Y.Z-dev")
	}
	for key, version := range map[string]string{
		"RANCHER_VERSION":       p.RancherVersion,
		"DOCKER_LINUX_VERSION":  p.DockerLinuxVersion,
		"COMPOSE_LINUX_VERSION": p.ComposeLinuxVersion,
	} {
		if !numericPattern.MatchString(version) {
			return nil, fmt.Errorf("%s must be X.Y.Z", key)
		}
	}
	if !gitWindowsPattern.MatchString(p.GitWindowsVersion) {
		return nil, fmt.Errorf("GIT_WINDOWS_VERSION must be X.Y.Z.windows.N")
	}
	for key, sum := range map[string]string{
		"RANCHER_MACOS_ARM64_SHA256":   p.RancherMacArm64SHA256,
		"RANCHER_MACOS_X86_64_SHA256":  p.RancherMacX8664SHA256,
		"RANCHER_WINDOWS_SHA256":       p.RancherWindowsSHA256,
		"GIT_WINDOWS_SHA256":           p.GitWindowsSHA256,
		"DOCKER_LINUX_X86_64_SHA256":   p.DockerLinuxX8664SHA256,
		"DOCKER_LINUX_AARCH64_SHA256":  p.DockerLinuxAarch64SHA256,
		"COMPOSE_LINUX_X86_64_SHA256":  p.ComposeLinuxX8664SHA256,
		"COMPOSE_LINUX_AARCH64_SHA256": p.ComposeLinuxAarch64SHA256,
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
