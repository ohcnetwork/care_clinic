package clinic

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/ohcnetwork/care_desktop/app/internal/compose"
	"github.com/ohcnetwork/care_desktop/app/internal/release"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

type Clinic struct {
	InstallDir string

	MDNSName          string
	AdminPassword     string
	BackupCertificate string
	BackupDir         string

	Pins *release.Pins

	Log     func(string)
	Confirm func(title, message string) bool
	Abandon func() bool

	localSetupOffered bool
}

func (e *Clinic) logln(s string) {
	if e.Log != nil {
		e.Log(s)
	}
}

func (e *Clinic) baseEnv() []string {
	env := os.Environ()
	set := func(k, v string) { env = append(env, k+"="+v) }
	set("PATH", proc.AugmentedPath())
	if host := proc.DockerHost(); host != "" {
		set("DOCKER_HOST", host)
		set("DOCKER_CONTEXT", "default")
	}
	set("COMPOSE_PROJECT_NAME", composeProject)
	set("COMPOSE_FILE", filepath.Join(e.InstallDir, "docker-compose.yml"))
	set("BACKEND_IMAGE", e.Pins.BackendImage)
	set("FRONTEND_IMAGE", e.Pins.FrontendImage)
	set("POSTGRES_IMAGE", e.Pins.PostgresImage)
	set("REDIS_IMAGE", e.Pins.RedisImage)
	set("MINIO_IMAGE", e.Pins.MinioImage)
	set("CADDY_IMAGE", e.Pins.CaddyImage)
	set("CADDY_WAF_IMAGE", e.Pins.CaddyWafImage)
	set("BACKUP_IMAGE", e.Pins.BackupImage)
	accessKey, secretKey := e.minioCreds()
	set("MINIO_ACCESS_KEY", accessKey)
	set("MINIO_SECRET_KEY", secretKey)
	set("CORAZA_MODE", e.corazaMode())

	benv := e.backendEnv()
	set("FILE_UPLOAD_BUCKET", strings.TrimSpace(benv["FILE_UPLOAD_BUCKET"]))
	set("FACILITY_S3_BUCKET", strings.TrimSpace(benv["FACILITY_S3_BUCKET"]))
	set("BACKUP_DIR", e.backupDir())
	return env
}

func (e *Clinic) minioCreds() (accessKey, secretKey string) {
	accessKey, secretKey = "minioadmin", "minioadmin"
	env := e.backendEnv()
	if v := strings.TrimSpace(env["BUCKET_KEY"]); v != "" {
		accessKey = v
	}
	if v := strings.TrimSpace(env["BUCKET_SECRET"]); v != "" {
		secretKey = v
	}
	return accessKey, secretKey
}

func (e *Clinic) backendEnv() map[string]string {
	env, _ := e.readBackendEnv()
	return env
}

func (e *Clinic) readBackendEnv() (map[string]string, error) {
	b, err := os.ReadFile(filepath.Join(e.InstallDir, "backend.env"))
	if err != nil {
		return nil, err
	}
	return dotenv.Parse(bytes.NewReader(b))
}

func (e *Clinic) corazaMode() string {
	v := strings.TrimSpace(e.backendEnv()["CORAZA_MODE"])
	switch strings.ToLower(v) {
	case "on":
		return "On"
	case "detectiononly":
		return "DetectionOnly"
	case "", "off":
		return "Off"
	}
	e.logln("CORAZA_MODE=" + v + " is not On, DetectionOnly or Off - using Off")
	return "Off"
}

func (e *Clinic) workdir() string {
	if st, err := os.Stat(e.InstallDir); err == nil && st.IsDir() {
		return e.InstallDir
	}
	return ""
}

func (e *Clinic) Runner() proc.Runner {
	return proc.Runner{Dir: e.workdir(), Env: e.baseEnv(), Log: e.Log}
}

func (e *Clinic) run(extraEnv []string, name string, args ...string) error {
	return e.Runner().RunWith(extraEnv, name, args...)
}

func (e *Clinic) capture(name string, args ...string) (string, error) {
	return e.Runner().Capture(name, args...)
}

func (e *Clinic) captureLines(name string, args ...string) ([]string, error) {
	return e.Runner().Lines(name, args...)
}

func (e *Clinic) dc(args ...string) error {
	return e.run(nil, "docker", append([]string{"compose"}, args...)...)
}

func (e *Clinic) mdnsName() string {
	if e.MDNSName != "" {
		return e.MDNSName
	}
	return "care"
}

func (e *Clinic) backupDir() string {
	if e.BackupDir != "" {
		return e.BackupDir
	}
	if desktop, err := proc.DesktopDir(); err == nil {
		return filepath.Join(desktop, "care-db-backups")
	} else {
		e.logln("Warning: could not locate the Desktop; using the legacy backup location: " + err.Error())
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Desktop", "care-db-backups")
}

func (e *Clinic) Label() string { return e.mdnsName() }

func (e *Clinic) Builder() *compose.Builder {
	b := compose.NewBuilder(e.Runner(), e.InstallDir, e.Pins, e.Log)
	b.Stop = e.Abandon
	return b
}
