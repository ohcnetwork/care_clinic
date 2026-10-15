package plugins

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const onboardingOnly = `[{"id":"care_onboarding_fe","label":"CARE Onboarding","catalog":true,"frontend":{"slug":"care_onboarding_fe","url":"https://ohcnetwork.github.io/care_onboarding_fe/assets/remoteEntry.js","meta":{"config":{"redirect_after_login":true}}}}]`

func existingClinic(t *testing.T, list string) *Manager {
	t.Helper()
	m := withEnv(t, "OTHER=kept\n")
	if list != "" {
		if err := os.WriteFile(m.listPath(), []byte(list), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func ids(list []Plugin) []string {
	out := []string{}
	for _, p := range list {
		out = append(out, p.ID)
	}
	return out
}

func TestUpgradeAddsNewDefaultOnce(t *testing.T) {
	m := existingClinic(t, onboardingOnly)
	added, err := m.AddNewDefaults()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids(added), []string{"care_multihost_files_be"}) {
		t.Fatalf("added %v", ids(added))
	}
	list, err := m.ReadPlugins()
	if err != nil || !reflect.DeepEqual(ids(list), []string{"care_onboarding_fe", "care_multihost_files_be"}) {
		t.Fatalf("saved plugins %v, %v", ids(list), err)
	}
	if backends, err := m.readBackends(); err != nil || !reflect.DeepEqual(backends, []Backend{multihostBackend}) {
		t.Fatalf("backend dependencies %#v, %v", backends, err)
	}
	if env, _ := os.ReadFile(m.backendEnvPath()); !strings.Contains(string(env), "OTHER=kept") {
		t.Fatalf("other settings were lost: %q", env)
	}
	if again, err := m.AddNewDefaults(); err != nil || len(again) != 0 {
		t.Fatalf("second run added %v, %v", ids(again), err)
	}
}

func TestUpgradeDoesNotReaddARemovedDefault(t *testing.T) {
	m := existingClinic(t, onboardingOnly)
	if _, err := m.AddNewDefaults(); err != nil {
		t.Fatal(err)
	}
	if err := m.SavePlugins([]Plugin{}); err != nil {
		t.Fatal(err)
	}
	if added, err := m.AddNewDefaults(); err != nil || len(added) != 0 {
		t.Fatalf("removed plugin came back: %v, %v", ids(added), err)
	}
	if list, err := m.ReadPlugins(); err != nil || len(list) != 0 {
		t.Fatalf("plugins changed: %v, %v", ids(list), err)
	}
}

func TestUpgradeSkipsAPluginAlreadyInstalledUnderAnotherID(t *testing.T) {
	m := existingClinic(t, `[{"id":"files","backend":{"name":"care_multihost_files_be","package_name":"git+https://example.org/fork.git"}}]`)
	added, err := m.AddNewDefaults()
	if err != nil || len(added) != 0 {
		t.Fatalf("added %v, %v", ids(added), err)
	}
	if list, _ := m.ReadPlugins(); !reflect.DeepEqual(ids(list), []string{"files"}) {
		t.Fatalf("plugins changed: %v", ids(list))
	}
	offered, err := m.readOffered()
	if err != nil || !offered["care_multihost_files_be"] {
		t.Fatalf("not recorded as offered: %v, %v", offered, err)
	}
}

func TestUpgradeWaitsForAPendingPluginDraft(t *testing.T) {
	m := existingClinic(t, onboardingOnly)
	if err := m.StagePlugins([]Plugin{}); err != nil {
		t.Fatal(err)
	}
	if added, err := m.AddNewDefaults(); err != nil || len(added) != 0 {
		t.Fatalf("added during a pending draft: %v, %v", ids(added), err)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, offeredFile)); !os.IsNotExist(err) {
		t.Fatalf("offer recorded during a pending draft: %v", err)
	}
	if err := m.DiscardPending(); err != nil {
		t.Fatal(err)
	}
	if added, err := m.AddNewDefaults(); err != nil || len(added) != 1 {
		t.Fatalf("not added after the draft cleared: %v, %v", ids(added), err)
	}
}

func TestUpgradeKeepsLegacyBackendPlugins(t *testing.T) {
	m := withEnv(t, `ADDITIONAL_PLUGS='[{"name":"care_x","package_name":"care-x","version":"==1.0"}]'`+"\n")
	if _, err := m.AddNewDefaults(); err != nil {
		t.Fatal(err)
	}
	backends, err := m.readBackends()
	want := []Backend{{Name: "care_x", PackageName: "care-x", Version: "==1.0"}, multihostBackend}
	if err != nil || !reflect.DeepEqual(backends, want) {
		t.Fatalf("backend dependencies %#v, %v", backends, err)
	}
}

func TestNewInstallIsNotOfferedDefaultsAgainOnUpgrade(t *testing.T) {
	m := withEnv(t, "OTHER=value\n")
	if err := m.InitializeDefaults(); err != nil {
		t.Fatal(err)
	}
	if err := m.SavePlugins([]Plugin{}); err != nil {
		t.Fatal(err)
	}
	if added, err := m.AddNewDefaults(); err != nil || len(added) != 0 {
		t.Fatalf("new install was offered defaults again: %v, %v", ids(added), err)
	}
}

func TestUpgradeReportsAnUnreadableOfferRecord(t *testing.T) {
	m := existingClinic(t, onboardingOnly)
	if err := os.WriteFile(filepath.Join(m.Dir, offeredFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddNewDefaults(); err == nil || !strings.Contains(err.Error(), offeredFile) {
		t.Fatalf("expected an error naming %s, got %v", offeredFile, err)
	}
}
