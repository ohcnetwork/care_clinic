package clinic

import (
	"errors"
	"fmt"
	"slices"

	"github.com/ohcnetwork/care_desktop/app/internal/compose"
)

func (e *Clinic) pruneBuildCache() error {
	e.logln("Pruning Docker build cache (shared with any other projects on this computer)...")
	if err := e.run(nil, "docker", "builder", "prune", "-f"); err != nil {
		return fmt.Errorf("could not prune the selected build cache: %w", err)
	}
	return nil
}

func (e *Clinic) uninstallImages() []string {
	return []string{
		e.Pins.BackendImage, e.Pins.FrontendImage, e.Pins.CaddyWafImage, e.Pins.BackupImage,
		e.Pins.BackendImage + "-next", e.Pins.FrontendImage + "-next",
		e.Pins.PostgresImage, e.Pins.RedisImage, e.Pins.MinioImage,
		e.Pins.CaddyImage, e.Pins.CaddyImage + "-builder",
	}
}

// publishedImages are the CARE images this clinic downloaded by digest.
func (e *Clinic) publishedImages() []string {
	lock := compose.ReadLock(e.InstallDir)
	var refs []string
	add := func(r compose.Release) {
		for _, ref := range []string{r.Image, r.Builder} {
			if ref != "" && !slices.Contains(refs, ref) {
				refs = append(refs, ref)
			}
		}
	}
	for _, c := range []compose.Channel{lock.Backend, lock.Frontend} {
		add(c.Current)
		if c.Next != nil {
			add(*c.Next)
		}
	}
	for _, ref := range []string{e.Pins.CaddyWafPrebuilt, e.Pins.BackupPrebuilt} {
		if ref != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}

func (e *Clinic) removeImages() error {
	available, err := e.captureLines("docker", "images", "--format", "{{.Repository}}:{{.Tag}}")
	if err != nil {
		return err
	}
	var failed []error
	for _, tag := range e.uninstallImages() {
		if slices.Contains(available, tag) {
			if err := e.removeImage(tag); err != nil {
				failed = append(failed, err)
			}
		}
	}
	for _, ref := range e.publishedImages() {
		if _, err := e.capture("docker", "image", "inspect", "--format", "{{.Id}}", ref); err != nil {
			continue
		}
		if err := e.removeImage(ref); err != nil {
			failed = append(failed, err)
		}
	}
	if err := e.pruneBuildCache(); err != nil {
		failed = append(failed, err)
	}
	return errors.Join(failed...)
}

func (e *Clinic) removeImage(tag string) error {
	if _, err := e.capture("docker", "image", "rm", tag); err != nil {
		return fmt.Errorf("could not remove image %s; leave shared images selected for keeping if another application uses them: %w", tag, err)
	}
	e.logln("  removed " + tag)
	return nil
}
