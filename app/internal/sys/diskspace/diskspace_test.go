package diskspace

import (
	"path/filepath"
	"testing"
)

func TestOfWalksUpToAnExistingFolder(t *testing.T) {
	dir := t.TempDir()
	u, err := Of(filepath.Join(dir, "not", "made", "yet"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != dir {
		t.Fatalf("path = %q, want %q", u.Path, dir)
	}
	if u.Total == 0 || u.Free > u.Total {
		t.Fatalf("implausible usage: %+v", u)
	}
	if u.Volume == "" {
		t.Fatalf("no volume id: %+v", u)
	}
	same, err := Of(dir)
	if err != nil {
		t.Fatal(err)
	}
	if same.Volume != u.Volume {
		t.Fatalf("volume ids differ for the same folder: %q vs %q", same.Volume, u.Volume)
	}
}

func TestOfRejectsEmptyPath(t *testing.T) {
	if _, err := Of(""); err == nil {
		t.Fatal("expected an error for an empty path")
	}
}
