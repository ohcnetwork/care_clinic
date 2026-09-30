package main

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/ohcnetwork/care_desktop/app/internal/release"
)

func TestEnsureInstallDirKeepsSettings(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	a := &App{
		configFile: filepath.Join(root, "care-desktop", "config.json"),
		pins:       &release.Pins{},
		installFS: fstest.MapFS{
			"install/.gitkeep":          {Data: []byte("")},
			"install/backend.env":       {Data: []byte("NEW=1\n")},
			"install/scripts/backup.sh": {Data: []byte("#!/bin/sh\n")},
		},
	}
	dest := a.installDir()
	for name, data := range map[string]string{
		"backend.env":  "USER_EDITED=1\n",
		"plugins.json": "[]\n",
	} {
		path := filepath.Join(dest, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := a.ensureInstallDir(); err != nil {
		t.Fatal(err)
	}

	if data, _ := os.ReadFile(filepath.Join(dest, "backend.env")); string(data) != "USER_EDITED=1\n" {
		t.Fatalf("operator settings were overwritten: %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(dest, "plugins.json")); string(data) != "[]\n" {
		t.Fatalf("operator plugin choices were overwritten: %q", data)
	}
	if info, err := os.Stat(filepath.Join(dest, "scripts", "backup.sh")); err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("shell script lost its execute bit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".gitkeep")); !os.IsNotExist(err) {
		t.Fatal("placeholder was installed")
	}
}
