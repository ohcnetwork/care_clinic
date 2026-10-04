package appremoval

import "testing"

func TestBundleOfAcceptsInstalledBundleOnly(t *testing.T) {
	cases := []struct {
		exe  string
		want string
	}{
		{"/Applications/CARE Clinic.app/Contents/MacOS/care-clinic", "/Applications/CARE Clinic.app"},
		{"/Users/a/Applications/CARE Clinic.app/Contents/MacOS/care-clinic", "/Users/a/Applications/CARE Clinic.app"},
		{"/Volumes/CARE Clinic/CARE Clinic.app/Contents/MacOS/care-clinic", ""},
		{"/private/var/folders/x/AppTranslocation/1/d/CARE Clinic.app/Contents/MacOS/care-clinic", ""},
		{"/Users/a/go/bin/care-clinic", ""},
	}
	for _, c := range cases {
		got, err := bundleOf(c.exe)
		if c.want == "" {
			if err == nil {
				t.Errorf("bundleOf(%q) = %q, want an error", c.exe, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("bundleOf(%q) = %q, %v; want %q", c.exe, got, err, c.want)
		}
	}
}

func TestOtherPIDsIgnoresOwnProcess(t *testing.T) {
	own := "\"CARE Clinic.exe\",\"4242\",\"Console\",\"1\",\"80,000 K\"\r\n"
	both := own + "\"CARE Clinic.exe\",\"77\",\"Console\",\"1\",\"90,000 K\"\r\n"
	none := "INFO: No tasks are running which match the specified criteria.\r\n"
	if otherPIDs(own, 4242) {
		t.Error("the uninstall process itself was treated as another instance")
	}
	if !otherPIDs(both, 4242) {
		t.Error("a second CARE Clinic process was not detected")
	}
	if otherPIDs(none, 4242) {
		t.Error("tasklist's no-match message was treated as a process")
	}
}

func TestDifferentUserComparesAccountsCaseInsensitively(t *testing.T) {
	if differentUser(`CLINIC-PC\Nurse`, `clinic-pc\nurse`) {
		t.Error("the same account in different case was treated as different")
	}
	if !differentUser(`CLINIC-PC\Nurse`, `CLINIC-PC\Admin`) {
		t.Error("an administrator approving the uninstall for another account was not detected")
	}
	if differentUser("", `CLINIC-PC\Admin`) {
		t.Error("an unknown console user must not block removal")
	}
}
