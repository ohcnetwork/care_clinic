package prereq

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// Windows only lets administrators write under HKCU\Software\Policies, so a
// profile put there never lands and Rancher Desktop falls back to its welcome
// dialog and a Kubernetes download.
func TestRancherProfileKeyAvoidsThePoliciesTree(t *testing.T) {
	if strings.Contains(strings.ToLower(rancherProfileKey), `\policies\`) {
		t.Fatalf("profile key needs elevation to write: %s", rancherProfileKey)
	}
	if !strings.HasSuffix(rancherProfileKey, `\Defaults`) {
		t.Fatalf("Rancher Desktop reads defaults from a Defaults subkey, got %s", rancherProfileKey)
	}
}

func TestRancherLaunchArgsSuppressDialogsAndKubernetes(t *testing.T) {
	args := rancherLaunchArgs()
	if args[0] != "--no-modal-dialogs" {
		t.Fatalf("first argument = %q, want --no-modal-dialogs", args[0])
	}
	if !strings.Contains(strings.Join(args, " "), "--kubernetes.enabled=false") {
		t.Fatalf("launch arguments must keep Kubernetes off: %v", args)
	}
	if len(rancherLaunchArgs()) != len(rancherSettings())+1 {
		t.Fatal("rancherLaunchArgs must pass every setting plus --no-modal-dialogs")
	}
}

// rdctl rejects --application.admin-access on Windows, and Rancher Desktop
// refuses to start when it is handed one. It is a Unix privileged-helper
// setting, so it must never reach the Windows command line or profile.
func TestAdminAccessIsUnixOnly(t *testing.T) {
	joined := strings.Join(rancherLaunchArgs(), " ")
	if runtime.GOOS == "windows" {
		if strings.Contains(joined, "admin-access") {
			t.Fatalf("Windows cannot accept admin-access: %s", joined)
		}
		return
	}
	if !strings.Contains(joined, "--application.admin-access=true") {
		t.Fatalf("admin access is still needed away from Windows: %s", joined)
	}
}

func TestRancherSettingsAreNotSharedBetweenCalls(t *testing.T) {
	first := rancherSettings()
	first[0] = "--mutated"
	if rancherSettings()[0] == "--mutated" {
		t.Fatal("callers can corrupt the settings used by every later launch")
	}
}

func TestRancherProfileSizesTheVirtualMachine(t *testing.T) {
	body := fmt.Sprintf(rancherProfilePlist, rancherProfileVersion, rancherMemoryGB)
	if strings.Contains(body, "%!") {
		t.Fatalf("profile template and its arguments disagree:\n%s", body)
	}
	if !strings.Contains(body, "<key>virtualMachine</key>\n\t<dict><key>memoryInGB</key><integer>3</integer></dict>") {
		t.Fatalf("profile does not give the VM 3 GB:\n%s", body)
	}
}

func TestRancherSwapOverrideIsAValidLimaProvisionScript(t *testing.T) {
	var override struct {
		Provision []struct {
			Mode   string `yaml:"mode"`
			Script string `yaml:"script"`
		} `yaml:"provision"`
	}
	if err := yaml.Unmarshal([]byte(rancherSwapOverride()), &override); err != nil {
		t.Fatal(err)
	}
	if len(override.Provision) != 1 || override.Provision[0].Mode != "system" {
		t.Fatalf("want one system provision script, got %+v", override.Provision)
	}
	script := override.Provision[0].Script
	if script != fmt.Sprintf(rancherSwapScript, rancherSwapGB) {
		t.Fatalf("the script changed on its way through YAML:\n%s", script)
	}
	if !strings.Contains(script, "fallocate -l 4G ") {
		t.Fatalf("swapfile is not 4 GB:\n%s", script)
	}
	if runtime.GOOS == "windows" {
		return
	}
	check := exec.Command("sh", "-n")
	check.Stdin = strings.NewReader(script)
	if out, err := check.CombinedOutput(); err != nil {
		t.Fatalf("swap script does not parse: %v\n%s", err, out)
	}
}

func TestRancherSwapOverrideKeepsAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lima", "_config", "override.yaml")
	if err := writeRancherSwapOverride(path); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != rancherSwapOverride() {
		t.Fatalf("override not written: %q, %v", got, err)
	}
	if err := os.WriteFile(path, []byte("cpus: 4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeRancherSwapOverride(path); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "cpus: 4\n" {
		t.Fatalf("an operator's own override was replaced: %q", got)
	}
}
