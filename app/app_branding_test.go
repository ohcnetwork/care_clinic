package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/plugins"
	"github.com/ohcnetwork/care_desktop/app/internal/release"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/trust"
	"go.yaml.in/yaml/v3"
)

func TestClinicBrandingContract(t *testing.T) {
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	var metadata struct {
		Name           string `json:"name"`
		OutputFilename string `json:"outputfilename"`
		Info           struct {
			ProductName string `json:"productName"`
		} `json:"info"`
	}
	if err := json.Unmarshal(read("wails.json"), &metadata); err != nil {
		t.Fatal(err)
	}
	for _, field := range []struct{ name, got, want string }{
		{"Wails project", metadata.Name, "care-clinic"},
		{"executable", metadata.OutputFilename, "CARE Clinic"},
		{"product", metadata.Info.ProductName, "CARE Clinic"},
		{"settings directory", appDirName, "care-clinic"},
		{"single-instance identity", singleInstanceID, "ohc.care-clinic"},
		{"plugin ownership", plugins.ManagedValue, "care-clinic"},
		{"certificate identity", trust.CommonName, "CARE Clinic Local CA"},
	} {
		if field.got != field.want {
			t.Errorf("%s = %q, want %q", field.name, field.got, field.want)
		}
	}
	var compose struct {
		Name     string `yaml:"name"`
		Networks map[string]struct {
			Name string `yaml:"name"`
		} `yaml:"networks"`
	}
	if err := yaml.Unmarshal(read("../deployments/docker-compose.yml"), &compose); err != nil {
		t.Fatal(err)
	}
	if compose.Name != appDirName || compose.Networks["default"].Name != appDirName {
		t.Error("Compose project and network must match the clinic identity")
	}
	if !strings.Contains(string(read("../deployments/Caddyfile")), `root_cn "`+trust.CommonName+`"`) {
		t.Error("Caddy's root certificate must match the native trust identity")
	}
	if _, err := release.Load(read("../deployments/.env")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"frontend/package.json", "frontend/package-lock.json"} {
		var pkg struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(read(path), &pkg); err != nil {
			t.Fatal(err)
		}
		if pkg.Name != "care-clinic-frontend" {
			t.Errorf("%s package name = %q", path, pkg.Name)
		}
	}
}
