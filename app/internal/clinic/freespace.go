package clinic

import (
	"slices"
	"strings"

	"github.com/ohcnetwork/care_desktop/app/internal/storage"
)

func (e *Clinic) FreeSpace() error {
	before, _, beforeErr := e.DockerDiskFree()

	if err := e.run(nil, "docker", "image", "prune", "-f"); err != nil {
		e.logln("Could not clear untagged images: " + err.Error())
	}

	available, err := e.captureLines("docker", "images", "--format", "{{.Repository}}:{{.Tag}}")
	if err != nil {
		e.logln("Could not list images: " + err.Error())
	}
	for _, tag := range staleImages(available, e.uninstallImages()) {
		if _, err := e.capture("docker", "image", "rm", tag); err != nil {
			e.logln("  kept " + tag + " (still in use)")
			continue
		}
		e.logln("  removed " + tag)
	}

	if err := e.pruneBuildCache(); err != nil {
		e.logln(err.Error())
	}

	after, _, afterErr := e.DockerDiskFree()
	switch {
	case beforeErr != nil || afterErr != nil:
		e.logln("Cleanup finished.")
	case after > before:
		e.logln("Cleanup finished. Freed " + storage.Human(after-before) + ".")
	default:
		e.logln("Cleanup finished. There was nothing left to clear.")
	}
	return nil
}

func staleImages(available, keep []string) []string {
	keepRefs := make([]string, 0, len(keep))
	repos := make([]string, 0, len(keep))
	for _, ref := range keep {
		repo, tag := splitImageRef(ref)
		keepRefs = append(keepRefs, repo+":"+tag)
		if !slices.Contains(repos, repo) {
			repos = append(repos, repo)
		}
	}
	var stale []string
	for _, ref := range available {
		repo, tag := splitImageRef(ref)
		if tag == "" || tag == "<none>" || !slices.Contains(repos, repo) {
			continue
		}
		full := repo + ":" + tag
		if !slices.Contains(keepRefs, full) && !slices.Contains(stale, full) {
			stale = append(stale, full)
		}
	}
	return stale
}

func splitImageRef(ref string) (repo, tag string) {
	ref = strings.TrimSpace(ref)
	if at := strings.Index(ref, "@"); at >= 0 {
		ref = ref[:at]
	}
	repo, tag = ref, "latest"
	if colon := strings.LastIndex(ref, ":"); colon > strings.LastIndex(ref, "/") {
		repo, tag = ref[:colon], ref[colon+1:]
	}
	repo = strings.TrimPrefix(repo, "docker.io/")
	repo = strings.TrimPrefix(repo, "library/")
	return repo, tag
}
