package plugins

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStagingLeavesActiveInputsUntouched(t *testing.T) {
	dir := t.TempDir()
	env := "UNCHANGED='literal $secret'\n"
	if err := os.WriteFile(filepath.Join(dir, "backend.env"), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(dir)
	list := []Plugin{{ID: "candidate", Backend: &Backend{Name: "candidate", PackageName: "candidate"}}}
	if err := m.StagePlugins(list); err != nil {
		t.Fatal(err)
	}
	got, err := m.PendingPlugins()
	if err != nil || !reflect.DeepEqual(got, list) {
		t.Fatalf("staged list changed: %+v, %v", got, err)
	}
	active, err := m.ReadPlugins()
	if err != nil || len(active) != 0 {
		t.Fatalf("staged plugins became active: %+v, %v", active, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "backend.env")); err != nil || string(data) != env {
		t.Fatalf("staging modified environment: %v", err)
	}
	if err := m.StagePlugins([]Plugin{{ID: "invalid"}}); err == nil {
		t.Fatal("invalid draft replaced staged configuration")
	}
	if err := m.DiscardPending(); err != nil {
		t.Fatal(err)
	}
	if err := m.DiscardPending(); err != nil {
		t.Fatal(err)
	}
}
