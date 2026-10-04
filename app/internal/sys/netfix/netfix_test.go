package netfix

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

func TestProfileStatusRejectsUnknownState(t *testing.T) {
	for _, tc := range []struct {
		name    string
		out     string
		err     error
		ok      bool
		fixable bool
	}{
		{"failed read", "Private", errors.New("unavailable"), false, false},
		{"empty", "", nil, false, false},
		{"unexpected", "Unknown", nil, false, false},
		{"public and unknown", "Public\nUnknown", nil, false, false},
		{"public", "Public", nil, false, true},
		{"mixed", "Private\nPublic", nil, false, true},
		{"private", "Private\r\n", nil, true, false},
		{"domain", "DomainAuthenticated", nil, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := profileStatus(tc.out, tc.err)
			if !status.Applicable || status.OK != tc.ok || status.Fixable != tc.fixable {
				t.Fatalf("unexpected status: %+v", status)
			}
		})
	}
}

func validRules() []firewallRule {
	return []firewallRule{
		{Name: fwPrefix + "mDNS", Enabled: "True", Direction: "Inbound", Action: "Allow", Profile: 3, Protocol: "UDP", LocalPort: []string{"5353"}},
		{Name: fwPrefix + "HTTPS", Enabled: "True", Direction: "Inbound", Action: "Allow", Profile: 3, Protocol: "TCP", LocalPort: []string{"443"}},
		{Name: fwPrefix + "HTTP", Enabled: "True", Direction: "Inbound", Action: "Allow", Profile: 3, Protocol: "TCP", LocalPort: []string{"80"}},
	}
}

func TestRulesReadyRequiresEveryEnabledRule(t *testing.T) {
	if !rulesReady(validRules()) {
		t.Fatal("valid rules were rejected")
	}
	if rulesReady(nil) || rulesReady(validRules()[:1]) || rulesReady(validRules()[:2]) {
		t.Fatal("an incomplete rule set was accepted")
	}
	for _, tc := range []struct {
		name   string
		change func(*firewallRule)
	}{
		{"unowned name", func(r *firewallRule) { r.Name = "Another application HTTPS" }},
		{"disabled", func(r *firewallRule) { r.Enabled = "False" }},
		{"outbound", func(r *firewallRule) { r.Direction = "Outbound" }},
		{"blocked", func(r *firewallRule) { r.Action = "Block" }},
		{"wrong protocol", func(r *firewallRule) { r.Protocol = "UDP" }},
		{"wrong port", func(r *firewallRule) { r.LocalPort = []string{"444"} }},
		{"any port", func(r *firewallRule) { r.LocalPort = []string{"Any"} }},
		{"additional port", func(r *firewallRule) { r.LocalPort = []string{"443", "444"} }},
		{"private only", func(r *firewallRule) { r.Profile = 2 }},
		{"public", func(r *firewallRule) { r.Profile = 4 }},
		{"any profile", func(r *firewallRule) { r.Profile = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rules := validRules()
			tc.change(&rules[1])
			if rulesReady(rules) {
				t.Fatalf("modified rule was accepted: %+v", rules[1])
			}
		})
	}
	rules := validRules()
	rules[0].Protocol = "17"
	rules[1].Protocol = "6"
	if !rulesReady(rules) {
		t.Fatal("numeric protocols were rejected")
	}
	if rulesReady(append(validRules(), validRules()[1])) {
		t.Fatal("duplicate owned rules were accepted")
	}
	if !rulesReady(append(validRules(), firewallRule{Name: "Unrelated rule"})) {
		t.Fatal("an unrelated rule affected CARE's rule readiness")
	}
}

func TestReadRulesDistinguishesMalformedAndFailedReads(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n[ \"$FAIL_RULE_READ\" != yes ] || exit 5\nprintf '%s\\n' \"$RULES_JSON\"\n"
	if err := os.WriteFile(filepath.Join(dir, "powershell"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAIL_RULE_READ", "no")
	data, err := json.Marshal(validRules())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RULES_JSON", string(data))
	rules, err := readRules(proc.Runner{})
	if err != nil || !rulesReady(rules) {
		t.Fatalf("read valid rules: %v, %+v", err, rules)
	}
	t.Setenv("RULES_JSON", "not JSON")
	if _, err := readRules(proc.Runner{}); err == nil {
		t.Fatal("malformed firewall output was accepted")
	}
	t.Setenv("RULES_JSON", string(data))
	t.Setenv("FAIL_RULE_READ", "yes")
	if _, err := readRules(proc.Runner{}); err == nil {
		t.Fatal("failed firewall inspection was accepted")
	}
}

func TestEnsureRuleRepairsOnlyTheRequiredOwnedRule(t *testing.T) {
	script := ensureRule("HTTPS", "TCP", 443)
	for _, required := range []string{
		"$_.DisplayName -eq 'CARE Clinic HTTPS'",
		"$_.Enabled -eq 'True'",
		"$_.Direction -eq 'Inbound'",
		"$_.Action -eq 'Allow'",
		"[int]$_.Profile -eq 3",
		"[string]$port.Protocol -in @('TCP','6')",
		"@($port.LocalPort).Count -eq 1",
		"($port.LocalPort -join ',') -eq '443'",
		"$rules.Count -ne 1 -or $valid.Count -ne 1",
		"Where-Object { $_.DisplayName -eq 'CARE Clinic HTTPS' } | Remove-NetFirewallRule",
		"-Enabled True -Direction Inbound -Protocol TCP -LocalPort 443 -Action Allow -Profile Private,Domain",
	} {
		if !strings.Contains(script, required) {
			t.Errorf("repair script is missing %q: %s", required, script)
		}
	}
	if strings.Contains(script, "-like") || strings.Contains(script, "SilentlyContinue") {
		t.Fatalf("repair uses a broad filter or ignores errors: %s", script)
	}
}

func firewallCommandFixture(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
printf '%s\n' "$3" >> "$FIREWALL_TRACE"
[ "$FAIL_INSPECTION" != yes ] || exit 7
printf '%s\n' "$RULE_COUNT"
`
	if err := os.WriteFile(filepath.Join(dir, "powershell"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	trace := filepath.Join(dir, "calls")
	t.Setenv("FIREWALL_TRACE", trace)
	t.Setenv("FAIL_INSPECTION", "no")
	return trace
}

func TestRulePresenceCountsAnyLeftoverAndPropagatesUnknown(t *testing.T) {
	trace := firewallCommandFixture(t)
	for _, tc := range []struct {
		count string
		want  bool
		err   bool
	}{
		{"0", false, false},
		{"1", true, false},
		{"2", true, false},
		{"3", true, false},
		{"", false, true},
		{"unknown", false, true},
		{"-1", false, true},
	} {
		t.Run(tc.count, func(t *testing.T) {
			t.Setenv("RULE_COUNT", tc.count)
			present, err := inspectRules(proc.Runner{})
			if present != tc.want || (err != nil) != tc.err {
				t.Fatalf("present = %v, err = %v", present, err)
			}
		})
	}
	t.Setenv("RULE_COUNT", "0")
	t.Setenv("FAIL_INSPECTION", "yes")
	if _, err := inspectRules(proc.Runner{}); err == nil {
		t.Fatal("failed inspection became a clean firewall")
	}
	scripts, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"$ErrorActionPreference = 'Stop'", "-PolicyStore ActiveStore", "$_.DisplayName -like 'CARE Clinic *'"} {
		if !strings.Contains(string(scripts), required) {
			t.Fatalf("presence query is missing %q: %s", required, scripts)
		}
	}
	if strings.Contains(string(scripts), "Enabled") || strings.Contains(string(scripts), "Get-NetFirewallPortFilter") {
		t.Fatalf("presence query is filtering incomplete or disabled rules: %s", scripts)
	}
}

func stubElevation(t *testing.T, fail bool) *[]elevate.Step {
	t.Helper()
	var seen []elevate.Step
	previous := elevateSteps
	elevateSteps = func(steps []elevate.Step) error {
		seen = append(seen, steps...)
		if fail {
			return errors.New("approval declined")
		}
		return nil
	}
	t.Cleanup(func() { elevateSteps = previous })
	return &seen
}

func TestUndoRequiresVerifiedRemovalOfEveryOwnedRule(t *testing.T) {
	for _, tc := range []struct {
		name            string
		count           string
		inspectionFails bool
		elevationFails  bool
		wantError       bool
	}{
		{"all removed", "0", false, false, false},
		{"one leftover", "1", false, false, true},
		{"several leftovers", "2", false, false, true},
		{"unreadable", "0", true, false, true},
		{"malformed", "", false, false, true},
		{"elevation refused", "0", false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			firewallCommandFixture(t)
			steps := stubElevation(t, tc.elevationFails)
			t.Setenv("RULE_COUNT", tc.count)
			if tc.inspectionFails {
				t.Setenv("FAIL_INSPECTION", "yes")
			}
			if err := undoRules(proc.Runner{}); (err != nil) != tc.wantError {
				t.Fatalf("undo error = %v", err)
			}
			if len(*steps) != 1 {
				t.Fatalf("elevated %d steps, want 1", len(*steps))
			}
			removal := (*steps)[0].PS
			for _, required := range []string{"-PolicyStore PersistentStore", "CARE Clinic *", "Remove-NetFirewallRule"} {
				if !strings.Contains(removal, required) {
					t.Fatalf("removal is missing %q: %s", required, removal)
				}
			}
			if strings.Contains(removal, "SilentlyContinue") {
				t.Fatalf("removal ignores errors: %s", removal)
			}
		})
	}
}
