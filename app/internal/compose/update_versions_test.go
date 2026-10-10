package compose

import (
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/lines"
)

func TestCareVersionUpgradeRequiresCompatibleApp(t *testing.T) {
	current := lineFixture("3", 'a')
	next := lineFixture("3.1", 'b')
	next.MinAppVersion = "0.2.0"
	manifest := &lines.Manifest{Schema: lines.Schema, Lines: []lines.Line{current, next}}
	for _, version := range []string{"0.1.11", "0.1.12"} {
		if got := NewerLine(manifest, "3", version); got != "" {
			t.Fatalf("Clinic %s offered CARE %s before the required app update", version, got)
		}
		if err := current.SupportsApp(version); err != nil {
			t.Fatalf("CARE 3 stopped supporting patch updates: %v", err)
		}
	}
	if got := NewerLine(manifest, "3", "0.2.0"); got != "3.1" {
		t.Fatalf("Clinic 0.2.0 did not unlock CARE 3.1: %q", got)
	}
	if err := current.SupportsApp("0.2.0"); err != nil {
		t.Fatalf("updating Clinic forced the operator off CARE 3: %v", err)
	}
}
