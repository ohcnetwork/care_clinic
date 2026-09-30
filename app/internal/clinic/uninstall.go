package clinic

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/hosts"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/netfix"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/trust"
)

type UninstallOptions struct {
	RemoveImages            bool
	RemoveInstallDir        bool
	RemoveBackups           bool
	RemoveUnusedCertificate bool
}

func (e *Clinic) Uninstall(opts UninstallOptions) error {
	resources, err := e.inspectProject()
	if err != nil {
		return err
	}
	if opts.RemoveInstallDir && !opts.RemoveBackups {
		if err := e.Backups().PreserveBackupCertificate(); err != nil {
			return err
		}
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
		if err := e.TeardownProject(); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		for _, resource := range resources {
			if len(resource.ids) > 0 {
				return errors.New("clinic resources remain but this installation's compose file is missing; reopen CARE Desktop to restore its files before uninstalling")
			}
		}
	}

	var failed []error
	if opts.RemoveImages {
		e.logln("Removing Docker images...")
		if err := e.removeImages(); err != nil {
			failed = append(failed, err)
		}
	}
	for _, detail := range e.revertSystemChanges(rootPEM) {
		failed = append(failed, errors.New(detail))
	}
	if err := errors.Join(failed...); err != nil {
		return err
	}
	if opts.RemoveBackups {
		e.logln("Removing backups in " + e.backupDir())
		if err := e.Backups().DeleteBackups(); err != nil {
			return err
		}
	}
	if opts.RemoveInstallDir {
		if err := e.removeInstallFiles(opts.RemoveUnusedCertificate); err != nil {
			return err
		}
	}
	e.reportLeftovers(opts, nil)
	return nil
}

func (e *Clinic) removeInstallFiles(removeUnusedKey bool) error {
	if looksLikeSourceRepo(e.InstallDir) {
		e.logln("Install dir looks like a source checkout - left in place: " + e.InstallDir)
		return fmt.Errorf("the source checkout at %s was kept; its files must not be deleted automatically", e.InstallDir)
	}
	dir := filepath.Clean(e.InstallDir)
	if !filepath.IsAbs(dir) || !strings.EqualFold(filepath.Base(dir), "install") ||
		!strings.EqualFold(filepath.Base(filepath.Dir(dir)), "care-desktop") {
		return fmt.Errorf("refusing to delete an unrecognized installation directory: %s", dir)
	}
	if removeUnusedKey {
		if err := e.Backups().DiscardUnusedCertificate(); err != nil {
			return err
		}
	}
	e.logln("Removing installed files " + dir)
	return os.RemoveAll(dir)
}

func (e *Clinic) revertSystemChanges(rootPEM string) []string {
	if runtime.GOOS == "windows" {
		return e.revertSystemChangesWindows(rootPEM)
	}
	var failed []string
	if s := trust.Untrust(e.Log, e.Confirm, rootPEM); s != "" {
		failed = append(failed, s)
	}
	if s := hosts.Remove(e.Log, e.Confirm, e.host()); s != "" {
		failed = append(failed, s)
	}
	present, err := netfix.InspectRules(e.Runner())
	if err != nil {
		failed = append(failed, err.Error())
	} else if present {
		if err := netfix.Undo(e.Log); err != nil {
			failed = append(failed, err.Error())
		}
	}
	return failed
}

func (e *Clinic) revertSystemChangesWindows(rootPEM string) []string {
	var failed []string
	var steps []elevate.Step

	certStep, certNeed, err := trust.RemoveStepWindows(rootPEM)
	if err != nil {
		failed = append(failed, err.Error())
	} else if certNeed {
		steps = append(steps, certStep)
	}

	host := e.host()
	hostsStep, hostsCleanup, hostsNeed := hosts.RemoveStepWindows(host)
	defer hostsCleanup()
	if hostsNeed {
		steps = append(steps, hostsStep)
	}

	fwStep, fwNeed, err := netfix.UndoStepWindows(e.Runner())
	if err != nil {
		failed = append(failed, err.Error())
	} else if fwNeed {
		steps = append(steps, fwStep)
	}

	if len(steps) == 0 {
		return failed
	}

	e.logln("Removing this computer's certificate trust, hosts entry, and firewall rules (approve the prompt)...")
	elevateErr := elevate.Teardown(steps)
	detail := ""
	if elevateErr != nil {
		detail = " (" + elevateErr.Error() + ")"
	}

	if certNeed {
		if present, err := trust.Inspect(); err != nil {
			failed = append(failed, err.Error())
		} else if present {
			failed = append(failed, "The certificate \""+trust.CommonName+"\" is still trusted by this computer"+detail+
				". Remove it in certmgr.msc, or run as administrator: certutil -delstore Root \""+trust.CommonName+"\"")
		} else {
			e.logln("Removed CARE's certificate from this machine's trust store.")
		}
	}
	if hostsNeed {
		if s := hosts.Leftover(host); s != "" {
			failed = append(failed, s+detail)
		} else {
			e.logln("Removed the " + host + " hosts entry.")
		}
	}
	if fwNeed {
		if present, err := netfix.InspectRules(e.Runner()); err != nil {
			failed = append(failed, err.Error())
		} else if present {
			failed = append(failed, "CARE's firewall rules are still present"+detail)
		} else {
			e.logln("Removed CARE's firewall rules.")
		}
	}
	return failed
}
