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

func TestDeploymentSourcesAcceptBranchesAndCommits(t *testing.T) {
	data := string(deploymentPins(t))
	pins, err := Load([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if pins.BeRef == "" || pins.FeRef == "" {
		t.Fatal("deployment sources name no ref to follow")
	}
	pinned := strings.Replace(data, "CARE_BE_REF="+pins.BeRef,
		"CARE_BE_REF=a749b92794ac175db8839d3d75ca36402a196282", 1)
	if _, err := Load([]byte(pinned)); err != nil {
		t.Fatalf("pinned source was rejected: %v", err)
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
