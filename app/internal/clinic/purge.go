package clinic

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/autostart"
)

func (e *Clinic) Project() string { return composeProject }

func (e *Clinic) Images() []string { return e.uninstallImages() }

func (e *Clinic) Purge() error {
	if _, err := e.inspectProject(); err != nil {
		return err
	}
	if err := e.Backups().PreserveBackupCertificate(); err != nil {
		return err
	}
	rootPEM := ""

	if info, err := os.Stat(filepath.Join(e.InstallDir, "docker-compose.yml")); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("the installed compose file is not a regular file")
		}
		rootPEM = e.caddyRootPEM()
		e.logln("Removing containers, network, and data volumes...")
		if err := e.dc("down", "-v", "--remove-orphans"); err != nil {
			e.logln("  (compose down reported an error - continuing cleanup)")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := e.TeardownProject(); err != nil {
		return err
	}

	e.logln("Removing Docker images...")
	var failed []error
	if err := e.removeImages(); err != nil {
		failed = append(failed, err)
	}
	for _, detail := range e.revertSystemChanges(rootPEM) {
		failed = append(failed, errors.New(detail))
	}

	if autostart.Enabled() {
		e.logln("Removing the start-at-login entry...")
		if err := autostart.Set(false); err != nil {
			failed = append(failed, err)
		}
	}

	if err := errors.Join(failed...); err != nil {
		return err
	}
	return e.removeInstallFiles(false)
}
