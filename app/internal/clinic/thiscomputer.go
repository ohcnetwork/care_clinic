package clinic

import (
	"runtime"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/hosts"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/trust"
)

const gatewayReadyTimeout = 60 * time.Second

func (e *Clinic) setUpThisComputerEarly() {
	e.logln("Starting the secure gateway so this computer can be set up now...")
	if err := e.dc("up", "-d", "--wait", "--wait-timeout", "120", "--no-deps", "caddy"); err != nil {
		e.logln("Couldn't start the secure gateway yet (" + err.Error() + "); this computer will be set up once CARE starts.")
		return
	}
	host := e.host()
	deadline := time.Now().Add(gatewayReadyTimeout)
	for e.caddyRootPEM() == "" || !trust.HostServed(host) {
		if time.Now().After(deadline) {
			e.logln("The secure gateway hasn't made its certificate yet; this computer will be set up once CARE starts.")
			return
		}
		time.Sleep(time.Second)
	}
	e.setUpThisComputer()
}

func (e *Clinic) setUpThisComputer() {
	e.localSetupOffered = true
	host := e.host()

	hostsStep, needHosts := hosts.Step(e.Log, host)
	cert, cleanup, needTrust := trust.Step(e.Log, host, e.caddyRootPEM())
	defer cleanup()
	var planned []elevate.Step
	if needHosts {
		planned = append(planned, hostsStep)
	}
	if needTrust {
		planned = append(planned, cert.Step)
	}
	var err error
	if len(planned) > 0 {
		e.logln("Setting up this computer to open https://" + host + "/...")
		title, message := confirmPrompt(runtime.GOOS, host, planned)
		if e.Confirm == nil || !e.Confirm(title, message) {
			e.logln("Skipped - other devices can still use the clinic, but this computer's own " +
				"browser may not open https://" + host + "/. Starting CARE again will offer this once more.")
			return
		}
		var steps []elevate.Step
		if needHosts {
			steps = append(steps, hostsStep)
		}
		userTrustFirst := trustWithoutAdminFirst(runtime.GOOS, needHosts)
		if needTrust && (!userTrustFirst || !cert.TryWithoutAdmin()) {
			steps = append(steps, cert.Step)
		}
		err = elevate.Steps(steps)
		if needTrust && !userTrustFirst && !trust.HostTrusts(host) {
			e.logln("The system certificate store didn't take CARE's certificate; trying this user's keychain instead...")
			cert.TryWithoutAdmin()
		}
	}
	e.logln(localSetupResult(host, hosts.HasEntry(host), trust.HostTrusts(host), err))
}

func trustWithoutAdminFirst(goos string, elevatingAnyway bool) bool {
	return goos != "darwin" || !elevatingAnyway
}

func localSetupResult(host string, hostsReady, trustReady bool, err error) string {
	var incomplete []string
	if !hostsReady {
		incomplete = append(incomplete, "the local hosts entry")
	}
	if !trustReady {
		incomplete = append(incomplete, "certificate trust")
	}
	if len(incomplete) == 0 {
		return "This computer can now open https://" + host + "/."
	}
	detail := ""
	if err != nil {
		detail = " (" + err.Error() + ")"
	}
	return "Could not confirm " + strings.Join(incomplete, " and ") + detail +
		". Other devices are unaffected; try starting CARE again to retry local setup, or ask your administrator for help."
}

func confirmPrompt(goos, host string, steps []elevate.Step) (title, message string) {
	var what strings.Builder
	for _, s := range steps {
		what.WriteString("  •  " + s.What + "\n")
	}
	return "Set up " + host + " on this computer?",
		"To open https://" + host + " in this computer's own browser, CARE needs to:\n\n" +
			what.String() + "\n" + approvalNote(goos, len(steps)) + " Other devices are unaffected."
}

func approvalNote(goos string, changes int) string {
	switch goos {
	case "darwin":
		if changes > 1 {
			return "macOS will ask for your administrator password, then ask you to confirm trusting the certificate."
		}
		return "macOS will ask you to approve this with your password or Touch ID."
	case "windows":
		return "Windows will ask for permission."
	}
	return "This asks for your administrator password."
}
