// Package netfix reads and repairs the Windows network profile and firewall
// rules that let other LAN devices reach the clinic. See docs/architecture.md.
package netfix

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

// fwPrefix tags the firewall rules we create so uninstall can remove exactly ours.
const fwPrefix = "CARE Clinic "

// Status reports whether the active network lets other LAN devices reach the
// clinic. Applicable is false off Windows (the wizard hides the row); Fixable means
// FixNetwork can repair it in one elevated click.
type Status struct {
	Applicable bool   `json:"applicable"`
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
	How        string `json:"how"`
	Fixable    bool   `json:"fixable"`
}

var requiredRules = []struct {
	label string
	proto string
	port  int
}{
	{"mDNS", "UDP", 5353},
	{"HTTPS", "TCP", 443},
	{"HTTP", "TCP", 80},
}

type firewallRule struct {
	Name      string
	Enabled   string
	Direction string
	Action    string
	Profile   int
	Protocol  string
	LocalPort []string
}

func Check(run proc.Runner) Status {
	if runtime.GOOS != "windows" {
		return Status{Applicable: false, OK: true, Message: "not needed on this OS"}
	}
	out, err := run.Capture("powershell", "-NoProfile", "-Command",
		"$ErrorActionPreference = 'Stop'; Get-NetConnectionProfile | ForEach-Object { $_.NetworkCategory }")
	if status := profileStatus(out, err); !status.OK {
		return status
	}
	rules, err := readRules(run)
	if err != nil {
		return Status{Applicable: true, Message: "couldn't read the clinic's firewall rules",
			How: "Check Windows Firewall and try again before allowing other devices to connect."}
	}
	if !rulesReady(rules) {
		return Status{
			Applicable: true, OK: false, Fixable: true,
			Message: "the clinic's ports aren't open",
			How: "Windows blocks incoming connections unless a rule allows them. " +
				"Click Fix to open the clinic's ports.",
		}
	}
	return Status{Applicable: true, OK: true, Message: "other devices can reach this computer"}
}

func profileStatus(out string, err error) Status {
	unknown := Status{Applicable: true, Message: "couldn't determine the network profile",
		How: "Connect to the clinic's network and check again. Network access has not been confirmed."}
	if err != nil || strings.TrimSpace(out) == "" {
		return unknown
	}
	public := false
	for _, line := range strings.Split(out, "\n") {
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "", "private", "domainauthenticated":
		case "public":
			public = true
		default:
			return unknown
		}
	}
	if public {
		return Status{
			Applicable: true, OK: false, Fixable: true,
			Message: "other devices can't reach this computer",
			How: "Windows is treating this WiFi as public, so it hides this computer " +
				"from the tablets and phones in your clinic. Click Fix to allow them in.",
		}
	}
	return Status{Applicable: true, OK: true}
}

func readRules(run proc.Runner) ([]firewallRule, error) {
	out, err := run.Capture("powershell", "-NoProfile", "-Command",
		`$ErrorActionPreference = 'Stop'; ConvertTo-Json -Compress -InputObject @(`+
			`Get-NetFirewallRule -PolicyStore ActiveStore | Where-Object { $_.DisplayName -like '`+fwPrefix+`*' } | `+
			`ForEach-Object { $port = $_ | Get-NetFirewallPortFilter; [pscustomobject]@{ `+
			`Name = $_.DisplayName; Enabled = [string]$_.Enabled; Direction = [string]$_.Direction; `+
			`Action = [string]$_.Action; Profile = [int]$_.Profile; `+
			`Protocol = [string]$port.Protocol; LocalPort = @($port.LocalPort) } })`)
	if err != nil {
		return nil, err
	}
	var rules []firewallRule
	if err := json.Unmarshal([]byte(out), &rules); err != nil {
		return nil, err
	}
	return rules, nil
}

func rulesReady(rules []firewallRule) bool {
	for _, required := range requiredRules {
		matches := 0
		for _, rule := range rules {
			if !strings.EqualFold(rule.Name, fwPrefix+required.label) {
				continue
			}
			if !strings.EqualFold(rule.Enabled, "True") || !strings.EqualFold(rule.Direction, "Inbound") ||
				!strings.EqualFold(rule.Action, "Allow") || rule.Profile != 3 ||
				!strings.EqualFold(rule.Protocol, required.proto) && rule.Protocol != protocolNumber(required.proto) ||
				len(rule.LocalPort) != 1 || rule.LocalPort[0] != strconv.Itoa(required.port) {
				return false
			}
			matches++
		}
		if matches != 1 {
			return false
		}
	}
	return true
}

func Fix(log func(string)) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	parts := []string{
		`$ErrorActionPreference = 'Stop'`,
		`Get-NetConnectionProfile | Where-Object { $_.NetworkCategory -eq 'Public' } | Set-NetConnectionProfile -NetworkCategory Private`,
	}
	for _, rule := range requiredRules {
		parts = append(parts, ensureRule(rule.label, rule.proto, rule.port))
	}
	inner := strings.Join(parts, "; ")
	logln(log, "This network is protected. Updating it so other devices can reach the clinic (approve the prompt)...")
	if err := elevate.Steps([]elevate.Step{{PS: inner}}); err != nil {
		return fmt.Errorf("couldn't update the network settings (prompt may have been declined): %w", err)
	}
	if status := Check(proc.Runner{}); !status.OK {
		return fmt.Errorf("network settings are still incomplete: %s", status.Message)
	}
	return nil
}

func ensureRule(label, proto string, port int) string {
	name := elevate.PSQuote(fwPrefix + label)
	return fmt.Sprintf(
		`$rules = @(Get-NetFirewallRule -PolicyStore ActiveStore | Where-Object { $_.DisplayName -eq %[1]s }); `+
			`$valid = @($rules | Where-Object { $port = $_ | Get-NetFirewallPortFilter; `+
			`$_.Enabled -eq 'True' -and $_.Direction -eq 'Inbound' -and $_.Action -eq 'Allow' -and [int]$_.Profile -eq 3 -and `+
			`[string]$port.Protocol -in @('%[2]s','%[4]s') -and @($port.LocalPort).Count -eq 1 -and ($port.LocalPort -join ',') -eq '%[3]d' }); `+
			`if ($rules.Count -ne 1 -or $valid.Count -ne 1) { `+
			`Get-NetFirewallRule -PolicyStore PersistentStore | Where-Object { $_.DisplayName -eq %[1]s } | Remove-NetFirewallRule; `+
			`New-NetFirewallRule -PolicyStore PersistentStore -DisplayName %[1]s -Enabled True -Direction Inbound -Protocol %[2]s `+
			`-LocalPort %[3]d -Action Allow -Profile Private,Domain | Out-Null }`,
		name, proto, port, protocolNumber(proto))
}

func protocolNumber(proto string) string {
	if strings.EqualFold(proto, "TCP") {
		return "6"
	}
	return "17"
}

func undoStep() elevate.Step {
	inner := `$ErrorActionPreference = 'Stop'; Get-NetFirewallRule -PolicyStore PersistentStore | ` +
		`Where-Object { $_.DisplayName -like '` + fwPrefix + `*' } | Remove-NetFirewallRule`
	return elevate.Step{What: "remove CARE's firewall rules", PS: inner}
}

func UndoStepWindows(run proc.Runner) (step elevate.Step, need bool, err error) {
	present, err := inspectRules(run)
	if err != nil || !present {
		return elevate.Step{}, false, err
	}
	return undoStep(), true, nil
}

func Undo(log func(string)) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	logln(log, "Removing the clinic's firewall rules (approve the prompt)...")
	return undoRules(proc.Runner{Log: log})
}

var elevateSteps = elevate.Steps

func undoRules(run proc.Runner) error {
	if err := elevateSteps([]elevate.Step{undoStep()}); err != nil {
		return fmt.Errorf("couldn't remove the clinic's firewall rules (approval may have been declined): %w", err)
	}
	present, err := inspectRules(run)
	if err != nil {
		return err
	}
	if present {
		return fmt.Errorf("CARE firewall rules are still present")
	}
	return nil
}

func logln(log func(string), s string) {
	if log != nil {
		log(s)
	}
}

func RulesPresent(run proc.Runner) bool {
	present, err := InspectRules(run)
	return present || err != nil
}

func InspectRules(run proc.Runner) (bool, error) {
	if runtime.GOOS != "windows" {
		return false, nil
	}
	return inspectRules(run)
}

func inspectRules(run proc.Runner) (bool, error) {
	out, err := run.Capture("powershell", "-NoProfile", "-Command",
		`$ErrorActionPreference = 'Stop'; $rules = @(Get-NetFirewallRule -PolicyStore ActiveStore | `+
			`Where-Object { $_.DisplayName -like '`+fwPrefix+`*' }); $rules.Count`)
	if err != nil {
		return false, fmt.Errorf("couldn't inspect CARE firewall rules: %w", err)
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || count < 0 {
		return false, fmt.Errorf("couldn't inspect CARE firewall rules: invalid rule count %q", out)
	}
	return count > 0, nil
}
