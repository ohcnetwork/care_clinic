package clinic

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/release"
)

func TestBackupScriptSHAFollowsTheInstalledScript(t *testing.T) {
	dir := t.TempDir()
	e := &Clinic{InstallDir: dir, Pins: &release.Pins{}}
	if got := e.backupScriptSHA(); got != "" {
		t.Fatalf("missing script: got %q", got)
	}
	script := filepath.Join(dir, "scripts", "backup.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("echo old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := e.backupScriptSHA()
	if len(old) != 16 {
		t.Fatalf("hash %q is not 16 hex characters", old)
	}
	if !slices.Contains(e.baseEnv(), "BACKUP_SCRIPT_SHA="+old) {
		t.Fatal("compose environment does not carry the backup script hash")
	}
	if err := os.WriteFile(script, []byte("echo new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if e.backupScriptSHA() == old {
		t.Fatal("a changed script kept the same hash, so the backup container would not be recreated")
	}
}
