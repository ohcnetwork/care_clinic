package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/plugins"
)

func TestPluginsDoNotRequireDesktopPassword(t *testing.T) {
	a := settingsApp(t)
	a.cfg.AdminPwHash = ""
	want := []plugins.Plugin{{
		ID: "example",
		Frontend: &plugins.Frontend{
			Slug: "example", URL: "https://example.com/remoteEntry.js",
		},
	}}
	if err := a.SavePlugins(want); err != nil {
		t.Fatal(err)
	}
	got, err := a.ReadPlugins()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("plugins were not accessible without a password: %+v, %v", got, err)
	}
	if _, err := a.ReadEnv("backend", ""); err == nil {
		t.Fatal("opening plugin access also opened protected environment settings")
	}
}

func TestPluginWritesKeepLifecycleAndMutationGuards(t *testing.T) {
	for _, state := range []string{"not installed", "client", "removing", "restore", "busy", "closing"} {
		t.Run(state, func(t *testing.T) {
			a := settingsApp(t)
			switch state {
			case "not installed":
				a.cfg.SetupDone = false
			case "client":
				a.cfg = Config{Role: roleClient}
			case "removing":
				a.cfg.Removing = true
			case "restore":
				if err := os.WriteFile(filepath.Join(a.installDir(), "restore-state.json"), []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "busy":
				a.jobMu.Lock()
				defer a.jobMu.Unlock()
			case "closing":
				a.closing = true
			}
			if err := a.SavePlugins(nil); err == nil {
				t.Fatal("plugin write bypassed its guard")
			}
			if _, err := os.Stat(filepath.Join(a.installDir(), "plugins.json")); !os.IsNotExist(err) {
				t.Fatalf("blocked operation wrote plugin settings: %v", err)
			}
		})
	}
}
