package applog

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStreamRotationAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "containers", "backend.log")
	s, err := NewStream(path)
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("x"), int(maxSize)+123)
	if n, err := s.Write(data); err != nil || n != len(data) {
		t.Fatalf("write: %d %v", n, err)
	}
	for name, want := range map[string]int64{path: 123, path + ".1": maxSize} {
		info, err := os.Stat(name)
		if err != nil || info.Size() != want {
			t.Fatalf("%s: %v %v", name, info, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("not private: %v", info.Mode())
		}
	}
}

func TestRetentionKeepsActiveDescriptorsAndUnrelatedFiles(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"care-log.log", "engine-console.log", "engine-console.log.1", "settings.env"} {
		p := filepath.Join(root, name)
		if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-8 * 24 * time.Hour)
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := Maintain(root, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"care-log.log", "engine-console.log"} {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil || info.Size() != 0 {
			t.Fatalf("active log lost or not truncated: %s %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "engine-console.log.1")); !os.IsNotExist(err) {
		t.Fatal("expired rotation retained")
	}
	if data, err := os.ReadFile(filepath.Join(root, "settings.env")); err != nil || string(data) != "old" {
		t.Fatal("unrelated file changed")
	}
}

func TestRetentionEnforcesSharedBudget(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "guest-system.log")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(LogBudget + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if err := Maintain(root, time.Now()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil || info.Size() != 0 {
		t.Fatalf("budget not enforced: %v", err)
	}
}
