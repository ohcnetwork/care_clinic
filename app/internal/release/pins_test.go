package release

import (
	"os"
	"strings"
	"testing"
)

func deploymentPins(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../../deployments/.env")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDeploymentNamesAReleaseLine(t *testing.T) {
	data := string(deploymentPins(t))
	pins, err := Load([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if pins.LinesURL == "" || pins.DefaultLine == "" {
		t.Fatal("deployment names no release line to follow")
	}
	digest := "ghcr.io/ohcnetwork/care-clinic-caddy@sha256:" + strings.Repeat("a", 64)
	for _, tt := range []struct {
		name, from, to string
		ok             bool
	}{
		{"http lines", "CARE_LINES_URL=" + pins.LinesURL, "CARE_LINES_URL=http://example.com/lines.json", false},
		{"bad line", "CARE_DEFAULT_LINE=" + pins.DefaultLine, "CARE_DEFAULT_LINE=../x", false},
		{"prebuilt caddy", "CADDY_WAF_PREBUILT=" + pins.CaddyWafPrebuilt, "CADDY_WAF_PREBUILT=" + digest, true},
		{"tagged caddy", "CADDY_WAF_PREBUILT=" + pins.CaddyWafPrebuilt, "CADDY_WAF_PREBUILT=caddy:latest", false},
		{"tagged backup", "BACKUP_PREBUILT=" + pins.BackupPrebuilt, "BACKUP_PREBUILT=postgres:17", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			changed := strings.Replace(data, tt.from+"\n", tt.to+"\n", 1)
			if changed == data {
				t.Fatalf("fixture %q not found", tt.from)
			}
			got, err := Load([]byte(changed))
			if (err == nil) != tt.ok {
				t.Fatalf("Load() error = %v", err)
			}
			if tt.ok && got.CaddyWafPrebuilt != digest {
				t.Fatal("prebuilt image was not read")
			}
		})
	}
}

func TestLoadValidatesVersions(t *testing.T) {
	data := string(deploymentPins(t))
	pins, err := Load([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"0.0", "v0.1.0", "0.1.0-release", "01.1.0"} {
		t.Run(version, func(t *testing.T) {
			invalid := strings.Replace(data, "CARE_CLINIC_VERSION="+pins.AppVersion, "CARE_CLINIC_VERSION="+version, 1)
			if _, err := Load([]byte(invalid)); err == nil {
				t.Fatal("invalid version was accepted")
			}
		})
	}
}

func TestIsCommitRef(t *testing.T) {
	for ref, want := range map[string]bool{
		"a749b92794ac175db8839d3d75ca36402a196282": true,
		"A749B92794AC175DB8839D3D75CA36402A196282": true,
		"a749b927": false,
		"develop":  false,
		"v0.1.0":   false,
		"g749b92794ac175db8839d3d75ca36402a196282": false,
	} {
		if got := IsCommitRef(ref); got != want {
			t.Errorf("IsCommitRef(%q) = %v, want %v", ref, got, want)
		}
	}
}
