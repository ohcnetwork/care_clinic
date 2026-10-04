package clinic

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/plugins"
	"github.com/ohcnetwork/care_desktop/app/internal/release"
)

func TestPluginLiveReadOnlyPreflight(t *testing.T) {
	dir := os.Getenv("CARE_PLUGIN_READONLY_INSTALL_DIR")
	if dir == "" {
		t.Skip("read-only live preflight requires an explicit installed clinic path")
	}
	data, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	pins, err := release.Load(data)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	e := &Clinic{InstallDir: dir, Pins: pins, ctx: ctx}
	containers, err := e.pluginContainers()
	if err != nil {
		t.Fatal(err)
	}
	e.pluginBaseline = containers
	if err := e.waitPluginRuntime(); err != nil {
		t.Fatal(err)
	}
	catalog, err := plugins.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range catalog {
		if entry.Plugin.ID == "care_onboarding_fe" {
			if _, err := e.snapshotPluginRows([]plugins.Plugin{entry.Plugin}); err != nil {
				t.Fatal(err)
			}
			t.Logf("Read-only preflight passed for %d live containers; onboarding candidate registration snapshot is compatible. No configuration or runtime was changed.", len(containers))
			return
		}
	}
	t.Fatal("onboarding catalog entry missing")
}
