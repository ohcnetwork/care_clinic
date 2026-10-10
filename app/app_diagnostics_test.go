package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnosticsExportOnlyLogsAndVersions(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"care-log.log", "backend.env", "data.img"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "backend.env"), filepath.Join(root, "secret.log")); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "diagnostics.zip")
	if err := writeDiagnostics(root, dest, map[string]string{"app_version": "0.1.12"}); err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = z.Close() }()
	if len(z.File) != 2 {
		t.Fatalf("unexpected archive entries: %v", z.File)
	}
	for _, f := range z.File {
		if f.Name != "versions.json" && f.Name != "care-log.log" {
			t.Fatalf("sensitive file exported: %s", f.Name)
		}
	}
}
