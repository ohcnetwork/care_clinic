package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPrivateAtomicReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "private.json")
	for _, data := range []string{"encrypted first value", "encrypted replacement"} {
		if err := WritePrivate(path, []byte(data)); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != data {
			t.Fatal("private atomic replacement did not persist")
		}
		info, err := os.Stat(path)
		if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
			t.Fatal("private file is not owner-restricted")
		}
	}
	if err := WritePrivate(dir, []byte("cannot replace directory")); err == nil {
		t.Fatal("private write replaced a directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("failed private write left staging files")
	}
}

func TestAtomicReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	for _, value := range []string{`{"state":"old"}`, `{"state":"new"}`} {
		if err := Write(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != value {
			t.Fatalf("replacement failed: %s, %v", data, err)
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("incorrect permissions: %v, %v", info, err)
		}
	}
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Write(blocked, []byte("data"), 0o600); err == nil {
		t.Fatal("replacing a directory unexpectedly succeeded")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("temporary files were left behind: %v, %v", entries, err)
	}
}
