package plugins

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPluginEnvironmentRoundTrip(t *testing.T) {
	t.Setenv("CARE_PLUGIN_VARIABLE", "must-not-expand")
	dir := t.TempDir()
	m := New(dir)
	if _, err := m.ReadPlugins(); err == nil {
		t.Fatal("a missing environment file became an empty plugin list")
	}
	path := filepath.Join(dir, "backend.env")
	if err := os.WriteFile(path, []byte("OTHER=value\nADDITIONAL_PLUGS=[]\nexport ADDITIONAL_PLUGS=[]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := []Backend{{Name: "example", PackageName: "example", Configs: map[string]any{
		"value": "$CARE_PLUGIN_VARIABLE and O'Connor\\path",
	}}}
	if err := m.writeBackends(want); err != nil {
		t.Fatal(err)
	}
	got, err := m.readBackends()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("plugin values changed: %#v, %v", got, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "OTHER=value") || strings.Count(string(data), additionalPlugsKey+"=") != 1 {
		t.Fatalf("environment editing changed unrelated settings or left duplicates: %s, %v", data, err)
	}
	if err := m.writeBackends(nil); err != nil {
		t.Fatal(err)
	}
	if got, err := m.readBackends(); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("plugins were not removed: %#v, %v", got, err)
	}
}

func withEnv(t *testing.T, content string) *Manager {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "backend.env"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return New(dir)
}

func TestExistingBackendPluginsAreReadWithoutAList(t *testing.T) {
	m := withEnv(t, `ADDITIONAL_PLUGS='[{"name":"care_x","package_name":"care-x","version":"==1.0"}]'`+"\n")
	got, err := m.ReadPlugins()
	if err != nil {
		t.Fatal(err)
	}
	want := []Plugin{{ID: "care_x", Backend: &Backend{Name: "care_x", PackageName: "care-x", Version: "==1.0"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy plugins were not carried over: %#v", got)
	}
}

func TestNewInstallDefaultsRegisterOnboarding(t *testing.T) {
	m := withEnv(t, "OTHER=value\n")
	if err := m.InitializeDefaults(); err != nil {
		t.Fatal(err)
	}
	list, err := m.ReadPlugins()
	if err != nil || len(list) != 1 {
		t.Fatalf("expected only onboarding by default: %#v, %v", list, err)
	}
	p := list[0]
	if p.ID != "care_onboarding_fe" || p.Label != "CARE Onboarding" || !p.Catalog || p.Backend != nil {
		t.Fatalf("incorrect default plugin: %#v", p)
	}
	rows, err := m.FrontendRows()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]any{
		"care_onboarding_fe": {
			"name":     "care_onboarding_fe",
			"url":      "https://ohcnetwork.github.io/care_onboarding_fe/assets/remoteEntry.js",
			"config":   map[string]any{"redirect_after_login": true},
			ManagedKey: ManagedValue,
		},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("onboarding runtime configuration: %#v", rows)
	}
	if raw, err := m.AdditionalPlugs(); err != nil || raw != "" {
		t.Fatalf("frontend default changed backend dependencies: %q, %v", raw, err)
	}
	if err := m.InitializeDefaults(); err != nil {
		t.Fatal(err)
	}
	if got, err := m.ReadPlugins(); err != nil || !reflect.DeepEqual(got, list) {
		t.Fatalf("setup retry changed saved plugins: %#v, %v", got, err)
	}
}

func TestDefaultsDoNotChangeExistingPluginChoices(t *testing.T) {
	for _, content := range []string{"[]\n", "null\n",
		`[{"id":"care_onboarding_fe","label":"My Onboarding","frontend":{"slug":"care_onboarding_fe","url":"http://localhost:4178/assets/remoteEntry.js","meta":{"config":{"redirect_after_login":false}}}}]`,
	} {
		t.Run(content, func(t *testing.T) {
			m := withEnv(t, "OTHER=kept\n")
			if err := os.WriteFile(m.listPath(), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := m.InitializeDefaults(); err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(m.listPath()); err != nil || string(got) != content {
				t.Fatalf("existing choices were overwritten: %q, %v", got, err)
			}
			if got, _ := os.ReadFile(m.backendEnvPath()); string(got) != "OTHER=kept\n" {
				t.Fatalf("existing environment was rewritten: %q", got)
			}
		})
	}
}

func TestDefaultInitializationReportsUnreadableConfiguration(t *testing.T) {
	m := New(t.TempDir())
	if err := m.InitializeDefaults(); err == nil {
		t.Fatal("missing backend environment was ignored")
	}
	m = withEnv(t, "OTHER=kept\n")
	if err := os.WriteFile(m.listPath(), []byte("invalid JSON\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.InitializeDefaults(); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("invalid saved configuration was ignored: %v", err)
	}
	if got, err := os.ReadFile(m.listPath()); err != nil || string(got) != "invalid JSON\n" {
		t.Fatalf("invalid saved configuration was overwritten: %q, %v", got, err)
	}
}

func TestExistingInstallReadDoesNotEnableDefaults(t *testing.T) {
	m := withEnv(t, "OTHER=kept\n")
	rows, err := m.FrontendRows()
	if err != nil || len(rows) != 0 {
		t.Fatalf("ordinary read enabled a default: %#v, %v", rows, err)
	}
	if _, err := os.Stat(m.listPath()); !os.IsNotExist(err) {
		t.Fatalf("ordinary read persisted a plugin list: %v", err)
	}
}

func TestDefaultInitializationPreservesBackendPlugins(t *testing.T) {
	m := withEnv(t, `ADDITIONAL_PLUGS='[{"name":"care_x","package_name":"care-x","version":"==1.0"}]'`+"\n")
	before, err := m.readBackends()
	if err != nil {
		t.Fatal(err)
	}
	if err := m.InitializeDefaults(); err != nil {
		t.Fatal(err)
	}
	after, err := m.readBackends()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("backend plugins changed: %#v, %v", after, err)
	}
	if got, err := m.ReadPlugins(); err != nil || len(got) != 2 {
		t.Fatalf("defaults not added alongside backend plugin: %#v, %v", got, err)
	}
}

func TestDisplayNamesAllowSpacesWhileIDsRemainTechnical(t *testing.T) {
	m := withEnv(t, "")
	list := []Plugin{{ID: "care_onboarding_fe", Label: "CARE Onboarding", Frontend: &Frontend{
		Slug: "care_onboarding_fe", URL: "https://example.com/assets/remoteEntry.js",
	}}}
	if err := m.SavePlugins(list); err != nil {
		t.Fatal(err)
	}
	if got, err := m.ReadPlugins(); err != nil || !reflect.DeepEqual(got, list) {
		t.Fatalf("display name was not preserved: %#v, %v", got, err)
	}
	list[0].ID = "CARE Onboarding"
	if err := m.SavePlugins(list); err == nil || !strings.Contains(err.Error(), "plugin ID") {
		t.Fatalf("invalid technical ID should identify the field: %v", err)
	}
}

func TestSaveKeepsFrontendOnlyPluginsOutOfTheBackendImage(t *testing.T) {
	m := withEnv(t, "OTHER=value\n")
	list := []Plugin{
		{ID: "be", Backend: &Backend{Name: "care_be", PackageName: "git+https://example.com/be.git", Version: "@main"}},
		{ID: "fe", Frontend: &Frontend{Slug: "care_fe_only", URL: "https://example.com/assets/remoteEntry.js"}},
	}
	if err := m.SavePlugins(list); err != nil {
		t.Fatal(err)
	}
	raw, err := m.AdditionalPlugs()
	if err != nil || !strings.Contains(raw, "care_be") || strings.Contains(raw, "care_fe_only") {
		t.Fatalf("ADDITIONAL_PLUGS should list only backend parts: %q, %v", raw, err)
	}
	got, err := m.ReadPlugins()
	if err != nil || !reflect.DeepEqual(got, list) {
		t.Fatalf("the saved list changed: %#v, %v", got, err)
	}
	if err := m.SavePlugins(nil); err != nil {
		t.Fatal(err)
	}
	if raw, _ := m.AdditionalPlugs(); raw != "" {
		t.Fatalf("an empty list left ADDITIONAL_PLUGS behind: %q", raw)
	}
	if got, err := m.ReadPlugins(); err != nil || len(got) != 0 {
		t.Fatalf("an empty list did not read back empty: %#v, %v", got, err)
	}
}

func TestPrepareRejectsBrokenPlugins(t *testing.T) {
	be := func(name, pkg, version string) *Backend {
		return &Backend{Name: name, PackageName: pkg, Version: version}
	}
	fe := func(slug, url string, meta map[string]any) *Frontend {
		return &Frontend{Slug: slug, URL: url, Meta: meta}
	}
	const remote = "https://example.com/assets/remoteEntry.js"
	cases := map[string][]Plugin{
		"no parts":         {{ID: "empty"}},
		"no name":          {{ID: " ", Backend: be("care_a", "pkg", "")}},
		"version sans @":   {{ID: "a", Backend: be("care_a", "pkg", "main")}},
		"module with dash": {{ID: "a", Backend: be("care-a", "pkg", "")}},
		"no package":       {{ID: "a", Backend: be("care_a", "", "")}},
		"relative url":     {{ID: "a", Frontend: fe("a", "/assets/remoteEntry.js", nil)}},
		"url in meta":      {{ID: "a", Frontend: fe("a", remote, map[string]any{"url": remote})}},
		"duplicate id":     {{ID: "a", Backend: be("care_a", "pkg", "")}, {ID: "a", Backend: be("care_b", "pkg", "")}},
		"duplicate module": {{ID: "a", Backend: be("care_a", "pkg", "")}, {ID: "b", Backend: be("care_a", "pkg", "")}},
		"duplicate slug":   {{ID: "a", Frontend: fe("x", remote, nil)}, {ID: "b", Frontend: fe("x", remote, nil)}},
	}
	for name, list := range cases {
		if _, err := Prepare(list); err == nil {
			t.Errorf("%s: accepted %#v", name, list)
		}
	}
}

func TestCatalogPluginsTakeSourcesFromTheCatalogAndKeepSettings(t *testing.T) {
	catalog, err := Catalog()
	if err != nil || len(catalog) == 0 {
		t.Fatalf("catalog did not load: %v", err)
	}
	var entry Plugin
	for _, e := range catalog {
		if e.Plugin.Backend != nil && e.Plugin.Frontend != nil {
			entry = e.Plugin
		}
	}
	if entry.ID == "" {
		t.Skip("no catalog entry has both parts")
	}
	stale := Plugin{ID: entry.ID, Catalog: true,
		Backend:  &Backend{Name: "tampered", PackageName: "elsewhere", Configs: map[string]any{"KEY": "kept"}},
		Frontend: &Frontend{Slug: "tampered", URL: "https://elsewhere.example/x.js", Meta: map[string]any{"config": "kept"}},
	}
	got, err := Prepare([]Plugin{stale})
	if err != nil {
		t.Fatal(err)
	}
	p := got[0]
	if p.Backend.PackageName != entry.Backend.PackageName || p.Backend.Name != entry.Backend.Name ||
		p.Frontend.URL != entry.Frontend.URL || p.Frontend.Slug != entry.Frontend.Slug {
		t.Fatalf("catalog sources were not applied: %#v", p)
	}
	if p.Backend.Configs["KEY"] != "kept" || p.Frontend.Meta["config"] != "kept" {
		t.Fatalf("operator settings were lost: %#v", p)
	}
	if _, err := Prepare([]Plugin{{ID: "gone", Catalog: true, Backend: &Backend{Name: "care_gone", PackageName: "pkg"}}}); err != nil {
		t.Fatalf("a plugin dropped from the catalog should stay as a custom plugin: %v", err)
	}
}

func TestEveryCatalogEntryIsValid(t *testing.T) {
	catalog, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	list := make([]Plugin, 0, len(catalog))
	for _, e := range catalog {
		list = append(list, e.Plugin)
	}
	if _, err := Prepare(list); err != nil {
		t.Fatal(err)
	}
}

func TestFrontendRowsCarryURLAndOwnership(t *testing.T) {
	m := withEnv(t, "")
	list := []Plugin{
		{ID: "be", Backend: &Backend{Name: "care_be", PackageName: "pkg"}},
		{ID: "fe", Frontend: &Frontend{Slug: "care_fe_x", URL: "https://example.com/r.js",
			Meta: map[string]any{"config": map[string]any{"A": "b"}}}},
	}
	if err := m.SavePlugins(list); err != nil {
		t.Fatal(err)
	}
	rows, err := m.FrontendRows()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]any{"care_fe_x": {
		"name":     "care_fe_x",
		"url":      "https://example.com/r.js",
		"config":   map[string]any{"A": "b"},
		ManagedKey: ManagedValue,
	}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("frontend rows: %#v", rows)
	}
}
