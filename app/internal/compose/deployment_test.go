package compose

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

func TestSiloBootstrapUsesContainerCredentials(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX command fixtures")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(dir, "mc-calls")
	for name, script := range map[string]string{
		"silo": "#!/bin/sh\n[ \"$*\" = 'server /data --console-address :9001' ]\n",
		"curl": "#!/bin/sh\nexit 0\n",
		"mc":   "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$CARE_MINIO_TRACE\"\nprintf '%s\\n' '--' >> \"$CARE_MINIO_TRACE\"\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	entrypoint, err := filepath.Abs("../../../deployments/minio/entrypoint.sh")
	if err != nil {
		t.Fatal(err)
	}
	run := proc.Runner{Dir: dir, Env: append(os.Environ(),
		"PATH="+bin,
		"CARE_MINIO_TRACE="+trace,
		"MINIO_ROOT_USER=clinic-storage",
		"MINIO_ROOT_PASSWORD=custom $ storage password",
		"MINIO_ACCESS_KEY=stale-user",
		"MINIO_SECRET_KEY=stale-password",
		"FILE_UPLOAD_BUCKET=clinic-records",
		"FACILITY_S3_BUCKET=clinic-facilities",
	)}
	if _, err := run.Capture("/bin/sh", entrypoint); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	want := "alias\nset\nlocal\nhttp://localhost:9000\nclinic-storage\ncustom $ storage password\n--\n" +
		"mb\n-p\nlocal/clinic-records\n--\n" +
		"mb\n-p\nlocal/clinic-facilities\n--\n" +
		"anonymous\nset\ndownload\nlocal/clinic-facilities\n--\n"
	if string(data) != want {
		t.Fatalf("Silo bootstrap received different settings:\ngot  %q\nwant %q", data, want)
	}
}

func TestCaddyRoutesConfiguredBuckets(t *testing.T) {
	data, err := os.ReadFile("../../../deployments/Caddyfile")
	if err != nil {
		t.Fatal(err)
	}
	matcher := "@buckets path /{$FILE_UPLOAD_BUCKET:patient-bucket}/* /{$FACILITY_S3_BUCKET:facility-bucket}/*"
	if !strings.Contains(string(data), matcher) {
		t.Fatal("Caddy does not use the configured bucket names")
	}
}

func TestCaddyBootstrapServesMobileSetupAndPublicRoot(t *testing.T) {
	data, err := os.ReadFile("../../../deployments/Caddyfile")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "(bootstrap) {")
	end := strings.Index(text, "(site) {")
	if start < 0 || end <= start {
		t.Fatal("missing bootstrap route group")
	}
	bootstrap := text[start:end]
	for _, want := range []string{
		"@mobileSetup path /setup /setup/",
		"handle @mobileSetup {\n\t\troot * /setup\n\t\trewrite * /index.html\n\t\tfile_server\n\t}",
		"handle /root.crt {\n\t\theader Content-Type application/x-x509-ca-cert\n\t\troot * /data/caddy/pki/authorities/local\n\t\tfile_server\n\t}",
	} {
		if !strings.Contains(bootstrap, want) {
			t.Fatalf("missing bootstrap contract: %s", want)
		}
	}
	for _, forbidden := range []string{"Referer", "query", "redir", "root.crt*", "install-cert"} {
		if strings.Contains(bootstrap, forbidden) {
			t.Fatalf("bootstrap contains obsolete or unsafe directive: %s", forbidden)
		}
	}
	if !strings.Contains(text, ":80 {\n\timport bootstrap") {
		t.Fatal("public root is not available over HTTP")
	}
	compose, err := os.ReadFile("../../../deployments/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), "./setup:/setup:ro") {
		t.Fatal("mobile setup directory is not mounted read-only")
	}
	page, err := os.ReadFile("../../../deployments/setup/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="ios"`, `id="android"`, "Certificate Trust Settings", "/root.crt?ok=1"} {
		if !strings.Contains(string(page), want) {
			t.Fatalf("mobile setup is missing %s", want)
		}
	}
	if strings.Contains(string(page), "install-cert") {
		t.Fatal("mobile setup includes a retired desktop installer")
	}
}

func TestOnboardingIsServedByCARE(t *testing.T) {
	data, err := os.ReadFile("../../../deployments/Caddyfile")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "(site) {")
	end := strings.Index(text, ":80 {")
	if start < 0 || end <= start {
		t.Fatal("missing site route group")
	}
	site := text[start:end]
	if strings.Contains(text, "handle_path /onboarding") {
		t.Fatal("onboarding must be handled by the CARE frontend, not a static loader")
	}
	if !strings.Contains(site, "handle {\n\t\treverse_proxy frontend:80\n\t}") {
		t.Fatal("CARE frontend catch-all must serve the onboarding route")
	}
}

func TestComposePassesSharedStorageSettings(t *testing.T) {
	if !proc.Exists("docker") {
		t.Skip("Docker Compose is not installed")
	}
	if _, err := (proc.Runner{}).Capture("docker", "compose", "version", "--short"); err != nil {
		t.Skip("Docker Compose is not installed")
	}
	dir, err := filepath.Abs("../../../deployments")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name       string
		user       string
		password   string
		patient    string
		facility   string
		wantUser   string
		wantPass   string
		wantBucket string
		wantPublic string
	}{
		{"defaults", "", "", "", "", "minioadmin", "minioadmin", "patient-bucket", "facility-bucket"},
		{"custom", "clinic-storage", "custom-storage-password", "clinic-records", "clinic-facilities", "clinic-storage", "custom-storage-password", "clinic-records", "clinic-facilities"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			run := proc.Runner{Dir: dir, Env: append(os.Environ(),
				"MINIO_ACCESS_KEY="+tt.user,
				"MINIO_SECRET_KEY="+tt.password,
				"FILE_UPLOAD_BUCKET="+tt.patient,
				"FACILITY_S3_BUCKET="+tt.facility,
			)}
			output, err := run.Capture("docker", "compose", "--project-directory", dir,
				"--env-file", filepath.Join(dir, ".env"), "-f", filepath.Join(dir, "docker-compose.yml"),
				"config", "--format", "json")
			if err != nil {
				t.Fatal(err)
			}
			var config struct {
				Services map[string]struct {
					Environment map[string]string `json:"environment"`
				} `json:"services"`
			}
			if err := json.Unmarshal([]byte(output), &config); err != nil {
				t.Fatal(err)
			}
			for _, service := range []string{"minio", "caddy"} {
				env := config.Services[service].Environment
				if env["FILE_UPLOAD_BUCKET"] != tt.wantBucket || env["FACILITY_S3_BUCKET"] != tt.wantPublic {
					t.Errorf("%s does not receive the configured buckets", service)
				}
			}
			env := config.Services["minio"].Environment
			if env["MINIO_ROOT_USER"] != tt.wantUser || env["MINIO_ROOT_PASSWORD"] != tt.wantPass {
				t.Fatal("MinIO server credentials differ from configured credentials")
			}
		})
	}
}
