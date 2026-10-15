package clinic

import "testing"

func TestApplyHomeAccessSettingsMigratesManagedValues(t *testing.T) {
	dir := t.TempDir()
	writeDomainFile(t, dir, "frontend.env", "REACT_CARE_API_URL=https://north.local/\nREACT_TITLE=CARE\n")
	writeDomainFile(t, dir, "backend.env", "DEBUG=False\r\nCSRF_TRUSTED_ORIGINS=[\"https://north.local\"]\r\nBUCKET_EXTERNAL_ENDPOINT=https://north.local\r\n")
	e := &Clinic{InstallDir: dir, MDNSName: "north"}
	changed, err := e.ApplyHomeAccessSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected the settings to change")
	}
	if got, want := readDomainFile(t, dir, "frontend.env"), "REACT_CARE_API_URL=\nREACT_TITLE=CARE\n"; got != want {
		t.Errorf("frontend.env:\ngot  %q\nwant %q", got, want)
	}
	if got, want := readDomainFile(t, dir, "backend.env"), "DEBUG=False\r\nCSRF_TRUSTED_ORIGINS=[\"https://north.local\",\"https://*.ts.net:8443\"]\r\nBUCKET_EXTERNAL_ENDPOINT=https://north.local\r\n"; got != want {
		t.Errorf("backend.env:\ngot  %q\nwant %q", got, want)
	}
	changed, err = e.ApplyHomeAccessSettings()
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("a second run should change nothing")
	}
}

func TestApplyHomeAccessSettingsKeepsCustomValues(t *testing.T) {
	dir := t.TempDir()
	frontend := "REACT_CARE_API_URL=https://care.example.org\n"
	backend := "CSRF_TRUSTED_ORIGINS='[\"https://north.local\",\"https://*.ts.net:8443\"]'\n"
	writeDomainFile(t, dir, "frontend.env", frontend)
	writeDomainFile(t, dir, "backend.env", backend)
	e := &Clinic{InstallDir: dir, MDNSName: "north"}
	changed, err := e.ApplyHomeAccessSettings()
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("custom values should be left alone")
	}
	if got := readDomainFile(t, dir, "frontend.env"); got != frontend {
		t.Errorf("frontend.env changed to %q", got)
	}
	if got := readDomainFile(t, dir, "backend.env"); got != backend {
		t.Errorf("backend.env changed to %q", got)
	}
}

func TestApplyHomeAccessSettingsKeepsQuoting(t *testing.T) {
	dir := t.TempDir()
	writeDomainFile(t, dir, "backend.env", "CSRF_TRUSTED_ORIGINS='[\"https://north.local\"]'\n")
	e := &Clinic{InstallDir: dir, MDNSName: "north"}
	if _, err := e.ApplyHomeAccessSettings(); err != nil {
		t.Fatal(err)
	}
	if got, want := readDomainFile(t, dir, "backend.env"), "CSRF_TRUSTED_ORIGINS='[\"https://north.local\",\"https://*.ts.net:8443\"]'\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestApplyHomeAccessSettingsWithoutFiles(t *testing.T) {
	e := &Clinic{InstallDir: t.TempDir(), MDNSName: "north"}
	changed, err := e.ApplyHomeAccessSettings()
	if err != nil || changed {
		t.Errorf("changed=%v err=%v", changed, err)
	}
}
