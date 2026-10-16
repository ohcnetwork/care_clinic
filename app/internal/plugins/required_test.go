package plugins

import (
	"reflect"
	"strings"
	"testing"
)

func withRequired(e CatalogEntry) Plugin {
	p := e.Plugin
	if len(e.RequiredConfigs) == 0 {
		return p
	}
	b := *p.Backend
	b.Configs = map[string]any{}
	for k, v := range p.Backend.Configs {
		b.Configs[k] = v
	}
	for _, key := range e.RequiredConfigs {
		b.Configs[key] = "filled-" + strings.ToLower(key)
	}
	p.Backend = &b
	return p
}

func fillyEntry(t *testing.T) CatalogEntry {
	t.Helper()
	catalog, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range catalog {
		if e.Plugin.ID == "care_filly" {
			return e
		}
	}
	t.Fatal("Filly is not in the catalog")
	return CatalogEntry{}
}

func TestFillyCatalogEntry(t *testing.T) {
	e := fillyEntry(t)
	want := Backend{
		Name:        "care_filly",
		PackageName: "git+https://github.com/ohcnetwork/care_filly.git",
		Version:     "@develop",
		Configs:     map[string]any{"MEDISPEAK_API_KEY": "", "MEDISPEAK_BASE_URL": ""},
	}
	if e.Plugin.Label != "Filly" || e.Default || e.AddToExisting || e.Plugin.Frontend != nil ||
		e.Plugin.Backend == nil || !reflect.DeepEqual(*e.Plugin.Backend, want) {
		t.Fatalf("unexpected Filly entry: %#v", e)
	}
	if !reflect.DeepEqual(e.RequiredConfigs, []string{"MEDISPEAK_API_KEY", "MEDISPEAK_BASE_URL"}) {
		t.Fatalf("required settings: %v", e.RequiredConfigs)
	}
}

func TestRequiredCatalogSettingsMustBeFilled(t *testing.T) {
	filled := withRequired(fillyEntry(t))
	const base = "https://medispeak.example/api/v2"
	cases := map[string]map[string]any{
		"as shipped":  {"MEDISPEAK_API_KEY": "", "MEDISPEAK_BASE_URL": ""},
		"key missing": {"MEDISPEAK_BASE_URL": base},
		"key blank":   {"MEDISPEAK_API_KEY": " \t", "MEDISPEAK_BASE_URL": base},
		"key null":    {"MEDISPEAK_API_KEY": nil, "MEDISPEAK_BASE_URL": base},
		"url blank":   {"MEDISPEAK_API_KEY": "msk_live_x", "MEDISPEAK_BASE_URL": "  "},
		"no configs":  nil,
	}
	for name, configs := range cases {
		p := filled
		b := *p.Backend
		b.Configs = configs
		p.Backend = &b
		_, err := Prepare([]Plugin{p})
		if err == nil || !strings.HasPrefix(err.Error(), "Filly needs MEDISPEAK_") ||
			!strings.HasSuffix(err.Error(), " before it can be installed") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := Prepare([]Plugin{{ID: "care_filly", Catalog: true}}); err == nil ||
		err.Error() != "Filly needs MEDISPEAK_API_KEY before it can be installed" {
		t.Errorf("a catalog plugin without settings: %v", err)
	}
	if _, err := Prepare([]Plugin{filled}); err != nil {
		t.Fatalf("filled settings were rejected: %v", err)
	}
}

func TestRequiredSettingsApplyOnlyToCatalogPlugins(t *testing.T) {
	custom := Plugin{ID: "care_filly", Backend: &Backend{Name: "care_filly", PackageName: "care-filly"}}
	if _, err := Prepare([]Plugin{custom}); err != nil {
		t.Fatalf("a custom plugin was held to catalog rules: %v", err)
	}
}

func TestOperatorFillySettingsSurviveCatalogSources(t *testing.T) {
	p := Plugin{ID: "care_filly", Label: "Renamed", Catalog: true, Backend: &Backend{
		Name: "tampered", PackageName: "elsewhere", Version: "@main",
		Configs: map[string]any{
			"MEDISPEAK_API_KEY":  "msk_live_secret",
			"MEDISPEAK_BASE_URL": "https://medispeak.example/api/v2",
			"EXTRA":              true,
		},
	}}
	got, err := Prepare([]Plugin{p})
	if err != nil {
		t.Fatal(err)
	}
	b := got[0].Backend
	if got[0].Label != "Filly" || b.Name != "care_filly" || b.Version != "@develop" ||
		b.PackageName != "git+https://github.com/ohcnetwork/care_filly.git" {
		t.Fatalf("catalog sources were not applied: %#v", got[0])
	}
	if !reflect.DeepEqual(b.Configs, p.Backend.Configs) {
		t.Fatalf("operator settings changed: %#v", b.Configs)
	}
}

func TestSavedFillySettingsReachTheBackendEnvironment(t *testing.T) {
	m := withEnv(t, "OTHER=kept\n")
	if err := m.SavePlugins([]Plugin{{ID: "care_filly", Catalog: true}}); err == nil {
		t.Fatal("Filly was saved without its API key")
	}
	if list, err := m.ReadPlugins(); err != nil || len(list) != 0 {
		t.Fatalf("a rejected save changed the plugin list: %v, %v", ids(list), err)
	}
	if err := m.SavePlugins([]Plugin{withRequired(fillyEntry(t))}); err != nil {
		t.Fatal(err)
	}
	backends, err := m.readBackends()
	if err != nil || len(backends) != 1 || backends[0].Configs["MEDISPEAK_API_KEY"] != "filled-medispeak_api_key" {
		t.Fatalf("backend settings: %#v, %v", backends, err)
	}
}

func TestFillyIsNotAddedByDefaultFlows(t *testing.T) {
	fresh := withEnv(t, "OTHER=value\n")
	if err := fresh.InitializeDefaults(); err != nil {
		t.Fatal(err)
	}
	list, err := fresh.ReadPlugins()
	if err != nil || !reflect.DeepEqual(ids(list), []string{"care_onboarding_fe", "care_multihost_files_be"}) {
		t.Fatalf("new install plugins %v, %v", ids(list), err)
	}
	existing := existingClinic(t, onboardingOnly)
	added, err := existing.AddNewDefaults()
	if err != nil || !reflect.DeepEqual(ids(added), []string{"care_multihost_files_be"}) {
		t.Fatalf("upgrade added %v, %v", ids(added), err)
	}
	offered, err := existing.readOffered()
	if err != nil || offered["care_filly"] {
		t.Fatalf("Filly was marked as offered: %v, %v", offered, err)
	}
}
