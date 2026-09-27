package clinic

import (
	"slices"
	"testing"
)

func TestStaleImagesOnlyTouchesPinnedRepos(t *testing.T) {
	keep := []string{
		"postgres:17.10-alpine",
		"docker.io/pgsty/silo:RELEASE.2026-09-03T13-18-01Z",
		"care:clinic",
		"care:clinic-next",
	}
	available := []string{
		"postgres:17.10-alpine",
		"postgres:17.6-alpine",
		"pgsty/silo:RELEASE.2026-09-03T13-18-01Z",
		"pgsty/silo:RELEASE.2025-01-01T00-00-00Z",
		"care:clinic",
		"care:clinic-next",
		"care:old",
		"<none>:<none>",
		"mysql:8",
		"someone/postgres:16",
	}
	got := staleImages(available, keep)
	want := []string{"postgres:17.6-alpine", "pgsty/silo:RELEASE.2025-01-01T00-00-00Z", "care:old"}
	if !slices.Equal(got, want) {
		t.Fatalf("staleImages = %v, want %v", got, want)
	}
}

func TestSplitImageRef(t *testing.T) {
	cases := map[string][2]string{
		"postgres:17":                {"postgres", "17"},
		"docker.io/library/redis:8":  {"redis", "8"},
		"localhost:5000/care:clinic": {"localhost:5000/care", "clinic"},
		"localhost:5000/care":        {"localhost:5000/care", "latest"},
		"caddy:2.11@sha256:abc":      {"caddy", "2.11"},
	}
	for ref, want := range cases {
		repo, tag := splitImageRef(ref)
		if repo != want[0] || tag != want[1] {
			t.Errorf("splitImageRef(%q) = %q, %q; want %q, %q", ref, repo, tag, want[0], want[1])
		}
	}
}
