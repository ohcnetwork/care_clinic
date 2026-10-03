package clinic

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestBackupDefaultUsesWindowsDesktop(t *testing.T) {
	desktop, err := windows.KnownFolderPath(windows.FOLDERID_Desktop, 0)
	if err != nil {
		t.Fatal(err)
	}
	e := &Clinic{}
	if got, want := e.BackupDirPath(), filepath.Join(desktop, "care-db-backups"); got != want {
		t.Fatalf("default backup directory = %q, want Windows Desktop %q", got, want)
	}
	e.BackupDir = `D:\ClinicBackups\care-db-backups`
	if got := e.BackupDirPath(); got != e.BackupDir {
		t.Fatalf("configured backup directory changed: %q", got)
	}
}
