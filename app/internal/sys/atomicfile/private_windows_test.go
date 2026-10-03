package atomicfile

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPrivateWindowsDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	for _, contents := range []string{"initial", "replacement"} {
		if err := WritePrivate(path, []byte(contents)); err != nil {
			t.Fatal(err)
		}
		descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			t.Fatal(err)
		}
		dacl, _, err := descriptor.DACL()
		if err != nil || dacl == nil || dacl.AceCount != 1 || !strings.Contains(descriptor.String(), "D:P") {
			t.Fatal("private replacement did not retain its single-user protected DACL")
		}
	}
}
