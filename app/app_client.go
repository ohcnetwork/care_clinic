package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/hosts"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/mdns"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/trust"
)

// ClinicInfo is what a read-only look for a clinic found. Fingerprint is the
// clinic root's SHA-1, shown for support rather than as a comparison step;
// AlreadyTrusted says the connection needs no certificate installation.
type ClinicInfo struct {
	URL            string `json:"url"`
	Host           string `json:"host"`
	Fingerprint    string `json:"fingerprint"`
	AlreadyTrusted bool   `json:"already_trusted"`
}

// ClientPreflight is what connecting would clean up on this computer. It is
// read-only and needs no administrator approval.
type ClientPreflight struct {
	HostsEntry            bool   `json:"hosts_entry"`
	OldCertificate        bool   `json:"old_certificate"`
	UnfinishedServerSetup bool   `json:"unfinished_server_setup"`
	EngineLeftovers       string `json:"engine_leftovers"`
}

const selfAddressedClinic = "this computer is sending the clinic address to itself; connect to fix it"

const unfinishedServerSetup = "this computer has an unfinished clinic setup; remove it in Setup before connecting"

// clientRoleAvailable keeps the start page free of writes: a computer with no
// role can still look for a clinic and connect. Only an actual clinic setup,
// saved or half-installed on disk, blocks it.
func (a *App) clientRoleAvailable() error {
	if a.loadConfig().Role == roleServer {
		return errors.New(unfinishedServerSetup)
	}
	installed, err := a.installDirInUse()
	if err != nil {
		return err
	}
	if installed {
		return errors.New(unfinishedServerSetup)
	}
	return nil
}

func (a *App) clientContext() context.Context {
	if a.ctx == nil {
		return context.Background()
	}
	return a.ctx
}

// clinicDialAddress bypasses a hosts entry that still points the clinic name at
// this computer. mDNS answers come off the wire rather than from the resolver,
// so the real clinic is reached without changing the file first - which is what
// lets the hosts repair share one administrator prompt with the certificate
// work. An empty result means ordinary name resolution is fine.
func clinicDialAddress(host string) (string, error) {
	if !hosts.HasEntry(host) {
		return "", nil
	}
	ip, err := mdns.Resolve(strings.TrimSuffix(host, ".local"))
	if err != nil {
		return "", errors.New(selfAddressedClinic)
	}
	return ip.String(), nil
}

// FindClinic looks for a clinic and checks it, and changes nothing: no hosts
// file, no certificate store, no saved settings and no browser.
func (a *App) FindClinic(address string) (info ClinicInfo, err error) {
	err = a.withReadJob(func() error {
		if err := a.clientRoleAvailable(); err != nil {
			return err
		}
		clinicURL, err := trust.ClientURL(address)
		if err != nil {
			return err
		}
		host := strings.TrimPrefix(clinicURL, "https://")
		at, err := clinicDialAddress(host)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(a.clientContext(), 30*time.Second)
		defer cancel()
		root, err := trust.FetchClientCertificate(ctx, clinicURL, at)
		if err != nil {
			return err
		}
		if err := trust.CheckClientConnection(ctx, clinicURL, root, at); err != nil {
			return err
		}
		present, err := trust.ClientCertificatePresent(root)
		if err != nil {
			return err
		}
		info = ClinicInfo{
			URL: clinicURL, Host: host,
			Fingerprint: trust.SHA1Hex(root), AlreadyTrusted: present,
		}
		a.rememberClinicRoot(clinicURL, root)
		a.logln("Found the clinic at " + clinicURL + " (certificate " + info.Fingerprint + ").")
		return nil
	})
	return info, err
}

// rememberClinicRoot keeps the root FindClinic already validated in memory, so
// connecting does not repeat the trust-on-first-use download. It is deliberately
// not written to the settings file: nothing is pinned until Connect runs.
func (a *App) rememberClinicRoot(clinicURL, root string) {
	a.pendingMu.Lock()
	defer a.pendingMu.Unlock()
	a.pendingURL = clinicURL
	a.pendingRoot = root
}

func (a *App) clinicRoot(clinicURL string) string {
	a.pendingMu.Lock()
	defer a.pendingMu.Unlock()
	if a.pendingURL != clinicURL {
		return ""
	}
	return a.pendingRoot
}

// ClientReachability is the answer to "is the clinic answering right now?".
// A clinic that is switched off is Reachable false with a Detail, not an error:
// the connected screen polls this while it is open, and an ordinary overnight
// shutdown must not look like a fault.
type ClientReachability struct {
	Reachable bool   `json:"reachable"`
	CheckedAt int64  `json:"checked_at"`
	Detail    string `json:"detail"`
}

// ClientReachable makes one short verified HTTPS connection to the saved clinic
// and closes it. It takes no job lock, because a connection in progress must
// not turn a background poll into "something else is still running", and it
// changes nothing: no hosts file, no certificate store, no settings, no
// browser, no elevation, no retry.
func (a *App) ClientReachable() (state ClientReachability, err error) {
	defer a.logError(&err)
	cfg := a.loadConfig()
	state.CheckedAt = time.Now().Unix()
	if cfg.ClientURL == "" {
		return ClientReachability{}, errors.New("this computer is not connected to a clinic")
	}
	probe, err := trust.ProbeClient(a.clientContext(), cfg.ClientURL, cfg.ClientCertificate)
	if err != nil {
		return ClientReachability{CheckedAt: state.CheckedAt}, err
	}
	state.Reachable, state.Detail = probe.Reachable, probe.Detail
	return state, nil
}

// ClientPreflight reports what connecting will clean up. It runs no privileged
// command and reaches no network.
func (a *App) ClientPreflight() (result ClientPreflight, err error) {
	err = a.withReadJob(func() error {
		cfg := a.loadConfig()
		entry, err := hosts.Inspect()
		if err != nil {
			return err
		}
		host := strings.TrimPrefix(cfg.ClientURL, "https://")
		result.HostsEntry = entry || host != "" && hosts.HasEntry(host)
		stale, err := trust.OtherCARoots(trust.SHA1Hex(cfg.ClientCertificate))
		if err != nil {
			return err
		}
		result.OldCertificate = len(stale) > 0
		installed, err := a.installDirInUse()
		if err != nil {
			return err
		}
		result.UnfinishedServerSetup = cfg.Role == roleServer || installed
		result.EngineLeftovers = a.engineLeftovers()
		return nil
	})
	return result, err
}

// engineLeftovers is best effort and deliberately narrow: only what the
// container engine still holds. A computer without Docker has none, an engine
// that cannot answer is not worth blocking a client connection over, and the
// hosts entry and old certificates have fields of their own.
func (a *App) engineLeftovers() string {
	if !dockerInstalled() {
		return ""
	}
	report, err := a.scanResidue()
	if err != nil {
		return ""
	}
	var labels []string
	for _, trace := range report.Traces {
		switch trace.ID {
		case "containers", "volumes", "networks", "images":
			labels = append(labels, trace.Label)
		}
	}
	return strings.Join(labels, ", ")
}

func (a *App) ConnectClient(address string) error {
	return a.withJob(func() error {
		if err := a.clientRoleAvailable(); err != nil {
			return err
		}
		cfg := a.loadConfig()
		clinicURL, err := trust.ClientURL(address)
		if err != nil {
			return err
		}
		if cfg.ClientURL != "" && cfg.ClientURL != clinicURL {
			return errors.New("remove this computer's current clinic access before connecting to another clinic")
		}
		// Claim the role before elevation, so an interrupted connection can
		// still be retried or removed from the client screen.
		cfg.Role = roleClient
		if err := a.saveConfig(cfg); err != nil {
			return fmt.Errorf("could not save the clinic connection; no certificate was installed: %w", err)
		}
		a.emit("client-connect-progress", "finding")
		host := strings.TrimPrefix(clinicURL, "https://")
		step, cleanup, repairHosts, err := hosts.RemoveHostStep(host)
		defer cleanup()
		if err != nil {
			return err
		}
		at := ""
		if repairHosts {
			// Only a bypass lets the clinic answer before the file is repaired.
			// Without one the file has to be repaired first, which costs a
			// second administrator prompt but still connects.
			at, _ = clinicDialAddress(host)
		}
		batched := repairHosts && at != ""
		root, err := a.connectRoot(cfg, clinicURL, at, repairHosts && !batched)
		if err != nil {
			return err
		}
		a.emit("client-connect-progress", "connecting")
		present, err := trust.ClientCertificatePresent(root)
		if err != nil {
			return err
		}
		cfg.ClientURL = clinicURL
		cfg.ClientCertificate = root
		cfg.ClientCertificateOwned = cfg.ClientCertificateOwned || !present
		// Save ownership before elevation so interrupted setup can be retried or removed.
		if err := a.saveConfig(cfg); err != nil {
			return fmt.Errorf("could not save the clinic connection; no certificate was installed: %w", err)
		}
		var extra []elevate.Step
		if batched {
			extra = append(extra, step)
		}
		removed, trustErr := trust.ApplyClientTrust(root, extra)
		a.logRemovedRoots(removed)
		if trustErr != nil {
			return trustErr
		}
		if batched {
			if err := hosts.VerifyHostRemoved(a.logln, host); err != nil {
				return err
			}
		}
		// Cleanup and trust share an administrator batch. Neither is reported
		// as done until the whole batch and the hosts verification succeed.
		a.emit("client-connect-progress", "checking")
		// The OS prompt can take longer than the download timeout.
		if err := trust.CheckClientConnection(a.clientContext(), clinicURL, "", ""); err != nil {
			return err
		}
		a.logln("This client can open " + clinicURL)
		a.emit("client-connect-progress", "opening")
		a.OpenURL(clinicURL)
		return nil
	})
}

// connectRoot repairs the hosts file first when the clinic could not be reached
// around it, then supplies the root FindClinic already validated, the pinned
// root, or a fresh trust-on-first-use download.
func (a *App) connectRoot(cfg Config, clinicURL, at string, repairFirst bool) (string, error) {
	if repairFirst {
		if err := hosts.RemoveHost(a.logln, strings.TrimPrefix(clinicURL, "https://")); err != nil {
			return "", err
		}
	}
	root := cfg.ClientCertificate
	if root == "" {
		root = a.clinicRoot(clinicURL)
	}
	ctx, cancel := context.WithTimeout(a.clientContext(), 30*time.Second)
	defer cancel()
	if root == "" {
		fetched, err := trust.FetchClientCertificate(ctx, clinicURL, at)
		if err != nil {
			return "", err
		}
		root = fetched
	}
	if err := trust.CheckClientConnection(ctx, clinicURL, root, at); err != nil {
		return "", err
	}
	return root, nil
}

func (a *App) logRemovedRoots(removed []trust.CARoot) {
	for _, root := range removed {
		a.logln("Removed a certificate from a clinic this computer no longer uses: " + root.String())
	}
}

func (a *App) DisconnectClient() error {
	return a.withJob(func() error {
		cfg := a.loadConfig()
		if cfg.Role != roleClient {
			return errors.New("clinic access removal is only available on a client computer")
		}
		if cfg.ClientCertificateOwned {
			if err := trust.RemoveClientCertificate(cfg.ClientCertificate); err != nil {
				return err
			}
		}
		if err := a.resetConfigAfterUninstall(); err != nil {
			return fmt.Errorf("could not clear the saved clinic connection; try removing clinic access again: %w", err)
		}
		a.logln("Removed this client's saved clinic access and role. No clinic data was changed.")
		return nil
	})
}

func dockerInstalled() bool {
	return proc.Command("docker", "--version").Run() == nil
}
