package clinic

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestAddMissingBackendSettings(t *testing.T) {
	for _, tc := range []struct {
		name, before, after string
		added               []string
	}{
		{
			name:   "adds both to an older file",
			before: "DJANGO_DEBUG=False",
			after:  "DJANGO_DEBUG=False\nMAINTAIN_PATIENT_NAME_IDENTIFIER=True\nMAINTAIN_PATIENT_PHONE_NUMBER_IDENTIFIER=True\nDJANGO_EMAIL_BACKEND=django.core.mail.backends.dummy.EmailBackend\n",
			added:  []string{"MAINTAIN_PATIENT_NAME_IDENTIFIER", "MAINTAIN_PATIENT_PHONE_NUMBER_IDENTIFIER", "DJANGO_EMAIL_BACKEND"},
		},
		{
			name:   "keeps an operator's choice",
			before: "MAINTAIN_PATIENT_NAME_IDENTIFIER=False\n",
			after:  "MAINTAIN_PATIENT_NAME_IDENTIFIER=False\nMAINTAIN_PATIENT_PHONE_NUMBER_IDENTIFIER=True\nDJANGO_EMAIL_BACKEND=django.core.mail.backends.dummy.EmailBackend\n",
			added:  []string{"MAINTAIN_PATIENT_PHONE_NUMBER_IDENTIFIER", "DJANGO_EMAIL_BACKEND"},
		},
		{
			name:   "leaves a current file alone",
			before: "MAINTAIN_PATIENT_NAME_IDENTIFIER=True\nMAINTAIN_PATIENT_PHONE_NUMBER_IDENTIFIER=False\nDJANGO_EMAIL_BACKEND=custom.Backend\n",
			after:  "MAINTAIN_PATIENT_NAME_IDENTIFIER=True\nMAINTAIN_PATIENT_PHONE_NUMBER_IDENTIFIER=False\nDJANGO_EMAIL_BACKEND=custom.Backend\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "backend.env")
			if err := os.WriteFile(path, []byte(tc.before), 0o600); err != nil {
				t.Fatal(err)
			}
			added, err := (&Clinic{InstallDir: dir}).AddMissingBackendSettings()
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(added, tc.added) {
				t.Fatalf("added %v, want %v", added, tc.added)
			}
			got, _ := os.ReadFile(path)
			if string(got) != tc.after {
				t.Fatalf("backend.env = %q, want %q", got, tc.after)
			}
			if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
				t.Fatalf("permissions changed to %v", info.Mode().Perm())
			}
		})
	}
}

func TestAddMissingBackendSettingsWithoutFile(t *testing.T) {
	added, err := (&Clinic{InstallDir: t.TempDir()}).AddMissingBackendSettings()
	if err != nil || added != nil {
		t.Fatalf("added %v, err %v", added, err)
	}
}

func TestBundledBackendEnvEnablesIdentifiers(t *testing.T) {
	data, err := os.ReadFile("../../../deployments/backend.env")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "backend.env"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	added, err := (&Clinic{InstallDir: dir}).AddMissingBackendSettings()
	if err != nil || len(added) != 0 {
		t.Fatalf("new installs should already include every added setting: added %v, err %v", added, err)
	}
}
