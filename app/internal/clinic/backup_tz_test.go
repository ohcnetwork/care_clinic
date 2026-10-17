package clinic

import (
	"testing"
	"time"
)

func TestPosixTZ(t *testing.T) {
	for _, tc := range []struct {
		offset int
		want   string
	}{
		{19800, "<+0530>-05:30"},
		{0, "<+0000>-00:00"},
		{-18000, "<-0500>+05:00"},
		{-12600, "<-0330>+03:30"},
	} {
		now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.FixedZone("test", tc.offset))
		if got := posixTZ(now); got != tc.want {
			t.Errorf("offset %d: got %q, want %q", tc.offset, got, tc.want)
		}
	}
}
