package release

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

func TestReleaseWorkflowValidatesIdentity(t *testing.T) {
	if !proc.Exists("node") {
		t.Skip("Node.js is not installed")
	}

	script := `
const fs = require("fs");
const vm = require("vm");
const workflow = fs.readFileSync(".github/workflows/release.yml", "utf8");
const match = workflow.match(/          node <<'NODE'\n([\s\S]*?)\n          NODE/);
if (!match) throw new Error("Release identity validator is missing");
const validator = match[1].replace(/^          /gm, "");
const pins = fs.readFileSync("deployments/.env", "utf8");
const version = pins.match(/^CARE_CLINIC_VERSION=(.+)$/m)[1].trim();
const change = (key, value) => pins.replace(new RegExp("^" + key + "=.*$", "m"), key + "=" + value);
const cases = [
  {name: "manual release"},
  {name: "version bump", pins: change("CARE_CLINIC_VERSION", "1.2.3"), version: "1.2.3"},
  {name: "invalid version", pins: change("CARE_CLINIC_VERSION", "01.2.3"), fails: true},
  {name: "branch backend", pins: change("CARE_BE_REF", "develop")},
  {name: "branch frontend", pins: change("CARE_FE_REF", "develop")},
  {name: "empty backend ref", pins: change("CARE_BE_REF", ""), fails: true},
  {name: "backend ref with spaces", pins: change("CARE_BE_REF", "not a ref"), fails: true},
  {name: "development version", pins: change("CARE_CLINIC_VERSION", version + "-dev"), fails: true},
  {name: "missing version", pins: pins.replace(/^CARE_CLINIC_VERSION=.*\n/m, ""), fails: true},
  {name: "duplicate version", pins: pins + "\nCARE_CLINIC_VERSION=" + version + "\n", fails: true},
  {name: "empty duplicate version", pins: pins + "\nCARE_CLINIC_VERSION=\n", fails: true},
  {name: "duplicate frontend", pins: pins + "\nCARE_FE_REF=\n", fails: true},
];
for (const test of cases) {
  let output = "";
  let failed = false;
  try {
    vm.runInNewContext(validator, {
      require: name => {
        if (name !== "fs") throw new Error("Unexpected module");
        return {
          readFileSync: path => {
            if (path === "deployments/.env") return test.pins === undefined ? pins : test.pins;
            throw new Error("Unexpected input path");
          },
          appendFileSync: (path, value) => {
            if (path !== "output") throw new Error("Unexpected output path");
            output += value;
          },
        };
      },
      process: {env: {
        GITHUB_OUTPUT: "output",
      }},
    });
  } catch (error) {
    failed = true;
  }
  if (failed !== Boolean(test.fails)) throw new Error(test.name + ": unexpected validation result");
  if (!failed && output !== "version=" + (test.version || version) + "\n") throw new Error(test.name + ": incorrect artifact version");
}
`
	cmd := proc.Command("node", "-e", script)
	cmd.Dir = "../../.."
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("release workflow validation failed: %v\n%s", err, output)
	}
}

func TestCIRequiredGateRejectsIncompleteChecks(t *testing.T) {
	if !proc.Exists("node") {
		t.Skip("Node.js is not installed")
	}
	script := `
const fs = require("fs");
const vm = require("vm");
const workflow = fs.readFileSync(".github/workflows/ci.yml", "utf8");
const match = workflow.match(/node -e '([^'\n]+)'/);
if (!match) throw new Error("Required CI gate is missing");
for (const result of ["success", "failure", "cancelled", "skipped"]) {
  for (const name of ["lint", "go", "frontend", "native"]) {
    const jobs = Object.fromEntries(["lint", "go", "frontend", "native"].map(key => [key, {result: "success"}]));
    jobs[name].result = result;
    const process = {env: {RESULTS: JSON.stringify(jobs)}};
    vm.runInNewContext(match[1], {process, console: {log() {}}});
    if (Boolean(process.exitCode) !== (result !== "success")) {
      throw new Error(name + ": gate incorrectly handled " + result);
    }
  }
}
`
	cmd := proc.Command("node", "-e", script)
	cmd.Dir = "../../.."
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("required CI gate failed: %v\n%s", err, output)
	}
}

func TestReleaseManifestChecksumsEveryAsset(t *testing.T) {
	if !proc.Exists("node") {
		t.Skip("Node.js is not installed")
	}
	script := `
const fs = require("fs");
const vm = require("vm");
const crypto = require("crypto");
const assert = require("assert/strict");
const workflow = fs.readFileSync(".github/workflows/release.yml", "utf8");
const scripts = [...workflow.matchAll(/          node <<'NODE'\n([\s\S]*?)\n          NODE/g)];
assert.equal(scripts.length, 2);
for (const signing of ["false", "true"]) for (const windowsSigning of ["false", "true"]) {
const files = new Map([
  ["CARE-Clinic-1.2.3-macos.dmg", "macOS application"],
  ["CARE-Clinic-1.2.3-windows-amd64-setup.exe", "Windows installer"],
  ["release-config.env", "CARE_CLINIC_VERSION=1.2.3\n"],
]);
const key = path => {
  assert.ok(path.startsWith("release-assets/"));
  return path.slice("release-assets/".length);
};
const env = {
  SIGN_MACOS: signing, SIGN_WINDOWS: windowsSigning,
  VERSION: "1.2.3", GITHUB_SHA: "a".repeat(40),
  GITHUB_SERVER_URL: "https://github.com", GITHUB_REPOSITORY: "ohcnetwork/care_desktop",
  GITHUB_RUN_ID: "123",
};
vm.runInNewContext(scripts[1][1].replace(/^          /gm, ""), {
  process: {env},
  require: name => {
    if (name === "crypto") return crypto;
    assert.equal(name, "fs");
    return {
      readFileSync: path => {
        assert.ok(files.has(key(path)));
        return files.get(key(path));
      },
      writeFileSync: (path, data) => files.set(key(path), data),
      readdirSync: path => {
        assert.equal(path, "release-assets");
        return [...files.keys()];
      },
    };
  },
});
const hash = value => crypto.createHash("sha256").update(value).digest("hex");
const manifest = JSON.parse(files.get("release-manifest.json"));
assert.equal(manifest.version, env.VERSION);
assert.equal(manifest.source_commit, env.GITHUB_SHA);
assert.deepEqual(manifest.signing, {
  macos: signing === "true" ? "developer-id-notarized" : "ad-hoc",
  windows: windowsSigning === "true" ? "signpath-foundation" : "unsigned",
});
assert.equal(manifest.configuration_sha256, hash(files.get("release-config.env")));
assert.equal(manifest.run_url, "https://github.com/ohcnetwork/care_desktop/actions/runs/123");
const lines = files.get("SHA256SUMS").trim().split("\n");
assert.equal(lines.length, 4);
for (const name of [...files.keys()].filter(name => name !== "SHA256SUMS")) {
  assert.ok(lines.includes(hash(files.get(name)) + "  " + name), "Missing checksum for " + name);
}
}
`
	cmd := proc.Command("node", "-e", script)
	cmd.Dir = "../../.."
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("release manifest failed: %v\n%s", err, output)
	}
}

func TestMacSigningRetainsExistingConfiguration(t *testing.T) {
	data, err := os.ReadFile("../../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	for _, secret := range []string{
		"MACOS_CERT_P12", "MACOS_CERT_PASSWORD", "MACOS_SIGN_IDENTITY",
		"APPSTORE_PRIVATE_KEY", "APPSTORE_KEY_ID", "APPSTORE_ISSUER_ID",
	} {
		if !strings.Contains(workflow, "secrets."+secret) {
			t.Errorf("existing macOS signing secret %s is no longer consumed", secret)
		}
	}
	for _, required := range []string{
		"--options runtime", `notarize "$dmg"`, `xcrun stapler validate "$app"`,
		`xcrun stapler validate "$dmg"`, `if: always() && env.SIGN_MACOS == 'true'`,
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("macOS signing contract is missing %q", required)
		}
	}
}

func TestUnsignedReleaseStillCreatesDraft(t *testing.T) {
	data, err := os.ReadFile("../../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	job := regexp.MustCompile(`(?ms)^  publish:\n.*?(?:^  \S|\z)`).FindString(string(data))
	if job == "" {
		t.Fatal("the draft release job is missing")
	}
	for _, required := range []string{"!cancelled()", "needs.package.result == 'success'"} {
		if !strings.Contains(job, required) {
			t.Errorf("the draft job must still run when the Windows signing jobs are skipped; its condition is missing %q", required)
		}
	}
}

func TestWindowsInstallerRejectsDamagedDownloads(t *testing.T) {
	data, err := os.ReadFile("../../build/windows/installer/project.nsi")
	if err != nil {
		t.Fatal(err)
	}
	var checks []string
	for _, line := range strings.Split(string(data), "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && strings.EqualFold(fields[0], "CRCCheck") {
			checks = append(checks, strings.Join(fields, " "))
		}
	}
	if len(checks) != 1 || checks[0] != "CRCCheck force" {
		t.Fatalf("the installer must always verify itself before running (CRCCheck force), found %q", checks)
	}
}
