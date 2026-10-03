import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { expect, test, type Page } from "@playwright/test";
import type {} from "./fixtures/host";
import { downloadProblem } from "../src/lib/prerequisite-errors";
import type { SetupPage } from "../src/types";

const samplePassword = "ClinicTest123";
const forward = (page: Page) => page.getByRole("button", { name: "Continue", exact: true });
const install = (page: Page) => page.getByRole("button", { name: "Install", exact: true });
const calls = (page: Page, method: string) => page.evaluate((name) =>
  window.careTest.calls.filter((call) => call.method === name).length, method);
declare global {
  interface Window {
    restartDialogTest: { blocked: boolean; dismissals: number };
  }
}

test.beforeEach(async ({ page, baseURL }) => {
  // Keep an in-flight flow stable when another agent edits this shared checkout.
  const socketOrigin = new URL(baseURL ?? "http://127.0.0.1:41783").origin.replace(/^http/, "ws");
  await page.routeWebSocket(`${socketOrigin}/**`, (socket) => socket.send('{"type":"connected"}'));
  page.on("pageerror", (e) => { throw e; });
});

async function start(page: Page, query = "") {
  await page.goto(`/tests/fixtures/index.html?scenario=current${query}`);
  await expect(page.getByRole("heading", { name: "Set up CARE on this computer" })).toBeVisible();
}
async function client(page: Page) {
  await start(page);
  await page.getByRole("button", { name: "Connect to an existing server on the local network" }).click();
  await expect(page.getByRole("heading", { name: "Find your clinic's server" })).toBeVisible();
}
async function address(page: Page, platform = "darwin") {
  await page.goto(`/tests/fixtures/index.html?scenario=current&role=server&platform=${platform}`);
  await expect(page.getByRole("heading", { name: "Choosing the clinic address" })).toBeVisible();
  await expect(forward(page)).toBeEnabled();
}
async function backups(page: Page, platform = "darwin") {
  await address(page, platform);
  await forward(page).click();
  await expect(page.getByRole("heading", { name: "Setting up backups" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Choose where to save", exact: true })).toBeEnabled();
}
async function saveBackup(page: Page) {
  await page.getByRole("button", { name: "Choose where to save", exact: true }).click();
  await expect(page.getByText("Recovery file saved", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Select saved file", exact: true }).click();
  await expect(forward(page)).toBeEnabled();
}
async function admin(page: Page, platform = "darwin") {
  await backups(page, platform);
  await saveBackup(page);
  await forward(page).click();
  await expect(page.getByRole("heading", { name: "Creating the admin password" })).toBeVisible();
}
async function fillPassword(page: Page, password = samplePassword) {
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByLabel("Confirm password", { exact: true }).fill(password);
}

test("Windows recovery folders and backup capacity copy", async ({ page }) => {
  await backups(page, "windows");
  await estimatedBackupSpace(page);
  await expect(page.getByText(/room for about .* of backups/i)).toHaveCount(0);
  await expect(page.getByText(/Desktop may sync to OneDrive/)).toBeVisible();
  await saveBackup(page);
  await page.getByRole("button", { name: "Open folder", exact: true }).click();
  await expect.poll(() => page.evaluate(() => window.careTest.calls
    .filter((call) => call.method === "OpenSetupRecoveryFolder").map((call) => call.args))).toEqual([[false]]);
  await forward(page).click();
  await fillPassword(page);
  await page.getByRole("button", { name: "Choose where to save", exact: true }).click();
  await page.getByRole("button", { name: "Open folder", exact: true }).click();
  await expect.poll(() => page.evaluate(() => window.careTest.calls
    .filter((call) => call.method === "OpenSetupRecoveryFolder").map((call) => call.args))).toEqual([[false], [true]]);
});

test("Non-Windows backup presentation is unchanged", async ({ page }) => {
  await backups(page);
  await estimatedBackupSpace(page);
  await expect(page.getByText(/room for about .* of backups/i)).toBeVisible();
  await saveBackup(page);
  await expect(page.getByRole("button", { name: "Open folder", exact: true })).toHaveCount(0);
  await expect(page.getByText(/Desktop may sync to OneDrive/)).toHaveCount(0);
});

for (const step of ["backup", "admin"] as const) {
  test(`Windows ${step} folder launch failure does not invalidate recovery files`, async ({ page }) => {
    if (step === "backup") {
      await backups(page, "windows");
      await saveBackup(page);
    } else {
      await admin(page, "windows");
      await fillPassword(page);
      await page.getByRole("button", { name: "Choose where to save", exact: true }).click();
    }
    await expect(forward(page)).toBeEnabled();
    await page.evaluate(() => window.careTest.failNext(
      "OpenSetupRecoveryFolder", "couldn't open the recovery folder: private Explorer failure"));
    await page.getByRole("button", { name: "Open folder", exact: true }).click();
    const failure = page.getByRole("alert").filter({ hasText: "This step couldn't finish" });
    await expect(failure).toBeVisible();
    await expect(page.getByText(/damaged or incomplete|recovery file couldn't be checked|recovery codes couldn't be saved or checked|private Explorer failure/)).toHaveCount(0);
    await expect(forward(page)).toBeEnabled();
    await page.getByRole("button", { name: "Open folder", exact: true }).click();
    await expect(failure).toHaveCount(0);
    await expect.poll(() => calls(page, "OpenSetupRecoveryFolder")).toBe(2);
    await expect.poll(() => calls(page, "SaveSetupBackupRecovery")).toBe(1);
    expect(await calls(page, "ReplaceSetupBackupRecovery")).toBe(0);
    expect(await calls(page, "SaveAdminRecoveryCodes")).toBe(step === "admin" ? 1 : 0);
  });
}

async function estimatedBackupSpace(page: Page) {
  await page.evaluate(() => window.careTest.respond("BackupDirSpace", {
    dir: "/test-fixtures/CLINIC-BACKUP/care-db-backups", free: 418 * 2 ** 30, total: 500 * 2 ** 30,
    need: 6 * 2 ** 30, set_bytes: 2 * 2 ** 30, days_left: 190,
    shares_docker_drive: false, level: "ok", message: "",
  }));
  await page.getByRole("button", { name: "Change folder", exact: true }).click();
  await expect(page.getByText("/test-fixtures/CLINIC-BACKUP/care-db-backups", { exact: true })).toBeVisible();
}

async function review(page: Page, platform = "darwin") {
  await admin(page, platform);
  await fillPassword(page);
  const save = page.getByRole("button", { name: "Choose where to save", exact: true });
  await expect(save).toBeEnabled();
  await save.click();
  await expect(forward(page)).toBeEnabled();
  await forward(page).click();
  await expect(page.getByRole("heading", { name: "Review before installing" })).toBeVisible();
  await expect(install(page)).toBeEnabled();
}
async function mountRestartDialog(page: Page, disabled = false, blocked = false) {
  // Supply test-owned props while rendering the real dialog and CareProvider.
  await page.route("**/src/App.tsx*", (route) => route.fulfill({
    contentType: "application/javascript",
    body: `
      import { RestartDialog } from "/src/screens/setup/restart-dialog.tsx";
      window.restartDialogTest = { blocked: ${blocked}, dismissals: 0 };
      export function App() {
        return RestartDialog({
          plan: { needed: true, title: "Restart required", detail: "Restart to finish preparing Windows.", label: "Restart now" },
          onDismiss: () => { window.restartDialogTest.dismissals += 1; },
          disabled: ${disabled},
          isBlocked: () => window.restartDialogTest.blocked,
        });
      }
    `,
  }));
  await page.goto("/tests/fixtures/index.html?scenario=current");
  await expect(page.getByRole("alertdialog")).toBeVisible();
}
async function capture(page: Page, name: string) {
  if (!process.env.CARE_SCREENSHOTS_DIR) return;
  await mkdir(process.env.CARE_SCREENSHOTS_DIR, { recursive: true });
  await page.evaluate(() => document.fonts.ready);
  await page.screenshot({ path: join(process.env.CARE_SCREENSHOTS_DIR, `${name}.png`), animations: "disabled" });
}
async function fits(page: Page) {
  expect(await page.evaluate(() => {
    const root = document.querySelector(".onboarding-flow")!;
    const footer = document.querySelector(".on-foot")?.getBoundingClientRect();
    return {
      pageX: document.documentElement.scrollWidth > innerWidth,
      pageY: document.documentElement.scrollHeight > innerHeight,
      rootX: root.scrollWidth > root.clientWidth,
      innerX: [...root.querySelectorAll(".on-main, .on-body, .on-client-body, .on-rail, .on-left")]
        .some((element) => element.scrollWidth > element.clientWidth + 1),
      footer: !footer || (footer.left >= 0 && footer.right <= innerWidth + 1 && footer.bottom <= innerHeight + 1),
    };
  })).toEqual({ pageX: false, pageY: false, rootX: false, innerX: false, footer: true });
}

test("find is read-only, single-flight, and requires confirmation before connecting", async ({ page }) => {
  await client(page);
  await expect(page.getByLabel("Clinic address")).toBeFocused();
  await page.evaluate(() => window.careTest.hold("FindClinic"));
  await page.getByRole("button", { name: "Find server", exact: true }).evaluate((button: HTMLButtonElement) => { button.click(); button.click(); });
  await expect(page.getByText("Looking for care.local on your network…")).toBeVisible();
  await expect(page.getByRole("button", { name: "Back", exact: true })).toBeDisabled();
  expect(await calls(page, "FindClinic")).toBe(1);
  expect(await calls(page, "ConnectClient")).toBe(0);
  expect(await page.evaluate(() => window.careTest.state.role)).toBe("");
  await page.evaluate(() => window.careTest.release("FindClinic"));
  await expect(page.getByRole("heading", { name: "We found your clinic's server" })).toBeVisible();
  expect(await calls(page, "SelectRole")).toBe(0);
  await page.getByRole("button", { name: "Not this one" }).click();
  await expect(page.getByLabel("Clinic address")).toHaveValue("care");
  expect(await calls(page, "ConnectClient")).toBe(0);
});

for (const value of ["https://care.local:8000/login", "care.local/login", "care?x=1", "-clinic", "clinic-", "a".repeat(64)]) {
  test(`client rejects an invalid name: ${value.slice(0, 24)}`, async ({ page }) => {
    await client(page);
    await page.getByLabel("Clinic address").fill(value);
    await expect(page.getByRole("button", { name: "Find server" })).toBeDisabled();
    await page.getByLabel("Clinic address").press("Enter");
    expect(await calls(page, "FindClinic")).toBe(0);
    expect(await calls(page, "ConnectClient")).toBe(0);
  });
}

test("pasting a valid full clinic address strips only the supported wrapper", async ({ page }) => {
  await client(page);
  await page.getByLabel("Clinic address").evaluate((element) => {
    const clipboardData = new DataTransfer();
    clipboardData.setData("text/plain", "https://CARE-HOSPITAL.local/");
    element.dispatchEvent(new ClipboardEvent("paste", { clipboardData, bubbles: true, cancelable: true }));
  });
  await expect(page.getByLabel("Clinic address")).toHaveValue("care-hospital");
  await page.getByRole("button", { name: "Find server" }).click();
  await expect(page.getByRole("heading", { name: "We found your clinic's server" })).toBeVisible();
  expect(await page.evaluate(() => window.careTest.calls.find((call) => call.method === "FindClinic")?.args)).toEqual(["care-hospital.local"]);
});

for (const first of ["find", "update"] as const) {
  test(`client discovery and updating cannot race when ${first} starts first`, async ({ page }) => {
    await page.goto("/tests/fixtures/index.html?scenario=available&role=client");
    await expect(page.getByRole("button", { name: "Update now", exact: true })).toBeEnabled();
    await page.evaluate((action) => {
      window.careTest.hold("FindClinic");
      const button = (text: string) => [...document.querySelectorAll("button")].find((item) => item.textContent?.trim() === text)!;
      const find = button("Find server");
      const update = button("Update now");
      if (action === "find") { find.click(); update.click(); }
      else { update.click(); find.click(); }
    }, first);
    if (first === "find") {
      await expect.poll(() => calls(page, "FindClinic")).toBe(1);
      expect(await calls(page, "InstallAppUpdate")).toBe(0);
      await page.evaluate(() => window.careTest.release("FindClinic"));
      await expect(page.getByRole("heading", { name: "We found your clinic's server" })).toBeVisible();
    } else {
      await expect(page.getByText("Downloading CARE Desktop 0.1.6…", { exact: true })).toBeVisible();
      expect(await calls(page, "FindClinic")).toBe(0);
      expect(await calls(page, "ClientPreflight")).toBe(0);
    }
  });
}

for (const role of ["client", "server"] as const) {
  for (const phase of ["installer", "restarting"] as const) {
    test(`terminal ${phase} app-update keeps ${role} locked until ${phase === "installer" ? "acknowledgement" : "reopening"}`, async ({ page }) => {
      await page.setViewportSize({ width: 720, height: 560 });
      await page.goto(`/tests/fixtures/index.html?scenario=available&role=${role}`);
      const next = role === "client" ? page.getByRole("button", { name: "Find server", exact: true }) : forward(page);
      await expect(next).toBeEnabled();
      await page.getByRole("button", { name: "Update now", exact: true }).click();
      await expect(page.getByText("Downloading CARE Desktop 0.1.6…", { exact: true })).toBeVisible();
      await page.evaluate((value) => {
        window.careTest.progress({ phase: value, done: 0, total: 0 });
        window.careTest.finishUpdate();
      }, phase);
      const guidance = page.getByText(phase === "installer"
        ? "Follow it to finish updating, then reopen CARE Desktop."
        : "You'll be back here in a moment.", { exact: true });
      await expect(guidance).toBeVisible();
      await expect(next).toBeDisabled();
      await expect(page.getByRole("button", { name: "Back", exact: true })).toBeDisabled();
      await expect(page.getByLabel("Clinic address", { exact: true })).toBeDisabled();
      if (phase === "installer") {
        await expect(page.getByRole("button", { name: "OK", exact: true })).toBeEnabled();
      } else {
        await expect(page.getByRole("button", { name: /^(OK|Done|Dismiss|Later)$/ })).toHaveCount(0);
      }
      await next.evaluate((button: HTMLButtonElement) => button.click());
      for (const method of ["FindClinic", "ConnectClient", "RunSetup"]) expect(await calls(page, method)).toBe(0);
      await guidance.scrollIntoViewIfNeeded();
      await fits(page);
      await capture(page, `onboarding-${role}-update-${phase}-720x560`);
      if (phase === "installer") {
        await page.getByRole("button", { name: "OK", exact: true }).click();
        await expect(next).toBeEnabled();
        await expect(page.getByLabel("Clinic address", { exact: true })).toBeEnabled();
        await expect(page.getByText("Up to date", { exact: true })).toHaveCount(0);
      }
    });
  }
}

test("connection progress comes from host events and rejection is not connected", async ({ page }) => {
  await client(page);
  await page.getByRole("button", { name: "Find server" }).click();
  await expect(page.getByRole("button", { name: "Connect", exact: true })).toBeVisible();
  await page.evaluate(() => {
    window.careTest.hold("ConnectClient");
    window.careTest.failNext("ConnectClient", "could not install the clinic certificate: User canceled (-128)");
  });
  await page.getByRole("button", { name: "Connect", exact: true }).evaluate((button: HTMLButtonElement) => { button.click(); button.click(); });
  await expect(page.getByRole("heading", { name: "Connecting to care.local" })).toBeVisible();
  expect(await calls(page, "ConnectClient")).toBe(1);
  await page.evaluate(() => window.careTest.emit("client-connect-progress", "checking"));
  await expect(page.getByText("Making sure CARE opens safely")).toBeVisible();
  await expect(page.getByRole("heading", { name: "You're connected" })).toHaveCount(0);
  await page.evaluate(() => window.careTest.release("ConnectClient"));
  await expect(page.getByRole("alert")).toContainText("Permission was not given");
  await expect(page.getByText(/-128|User canceled/)).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Back", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: "Try again", exact: true }).click();
  await expect(page.getByRole("heading", { name: "You're connected" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Open CARE", exact: true })).toBeEnabled();
  await expect(page.getByRole("button", { name: "Back", exact: true })).toHaveCount(0);
  expect(await page.evaluate(() => window.careTest.state.client_url)).toBe("https://care.local");
});

for (const [raw, title] of [
  ["could not reach the clinic: dial 192.0.2.1: no route", "We couldn't find the clinic"],
  ["this is not a CARE clinic root certificate", "This doesn't look like a CARE clinic"],
  ["the clinic certificate is not valid now: internal details", "Check this computer's date and time"],
  ["could not verify the secure connection: x509 private details", "We couldn't connect securely"],
  ["could not save the clinic connection; no certificate was installed: EACCES", "The connection couldn't be saved"],
  ["something else is still running - private job", "Please wait a moment"],
]) {
  test(`client maps ${title} without exposing technical causes`, async ({ page }) => {
    await client(page);
    await page.evaluate((error) => window.careTest.failNext("FindClinic", error), raw);
    await page.getByRole("button", { name: "Find server" }).click();
    await expect(page.getByRole("alert")).toContainText(title);
    expect(await page.locator("body").innerText()).not.toContain(raw);
    expect(await calls(page, "ConnectClient")).toBe(0);
  });
}

test("unfinished server setup points to Setup and never runs client cleanup", async ({ page }) => {
  await client(page);
  await page.evaluate(() => {
    window.careTest.fixtures.preflight.unfinished_server_setup = true;
    window.careTest.state.role = "server";
  });
  await page.getByRole("button", { name: "Find server" }).click();
  await expect(page.getByRole("alert")).toContainText("This computer has an unfinished clinic setup");
  expect(await calls(page, "FindClinic")).toBe(0);
  expect(await calls(page, "PurgeResidue")).toBe(0);
  await page.getByRole("button", { name: "Open setup" }).click();
  await expect(page.getByRole("heading", { name: "Choosing the clinic address" })).toBeVisible();
  expect(await calls(page, "BeginServerSetup")).toBe(1);
});

test("an old self-address setting offers the backend repair path", async ({ page }) => {
  await client(page);
  await page.evaluate(() => {
    window.careTest.fixtures.preflight.hosts_entry = true;
    window.careTest.failNext("FindClinic", "this computer is sending the clinic address to itself; connect to fix it");
  });
  await page.getByRole("button", { name: "Find server" }).click();
  await page.getByRole("button", { name: "Connect and fix" }).click();
  await expect(page.getByRole("heading", { name: "You're connected" })).toBeVisible();
  expect(await calls(page, "PurgeResidue")).toBe(0);
});

test("saved clients poll reachability and recover without losing the connection", async ({ page }) => {
  await page.clock.install();
  await page.goto("/tests/fixtures/index.html?scenario=client-saved");
  await expect(page.getByRole("button", { name: "Open CARE" })).toBeEnabled();
  await page.evaluate(() => {
    window.careTest.fixtures.reachability.reachable = false;
    window.careTest.fixtures.reachability.detail = "the server did not answer";
  });
  await page.clock.fastForward(15_100);
  await expect(page.getByText("This computer is set up for care.local, but we can't find it right now")).toBeVisible();
  await expect(page.getByRole("button", { name: "Open CARE" })).toBeDisabled();
  expect(await page.evaluate(() => window.careTest.state.client_url)).toBe("https://care.local");
  await page.evaluate(() => { window.careTest.fixtures.reachability.reachable = true; });
  await page.getByRole("button", { name: "Check again", exact: true }).click();
  await expect(page.getByRole("button", { name: "Open CARE" })).toBeEnabled();
  expect(await calls(page, "DisconnectClient")).toBe(0);
});

test("an unavailable clipboard shows a friendly error without throwing or losing the saved clinic", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?scenario=client-saved");
  await page.evaluate(() => Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined }));
  await page.getByRole("button", { name: "Copy address", exact: true }).click();
  await expect(page.getByRole("alert")).toHaveText("Couldn't copy the address. You can select it and copy it yourself.");
  expect(await page.evaluate(() => window.careTest.state.client_url)).toBe("https://care.local");
  expect(await page.evaluate(() => window.careTest.logs.some((line) => line.includes("copy clinic address:")))).toBe(true);
  expect(await calls(page, "DisconnectClient")).toBe(0);
});

test("a failed post-disconnect navigation does not leave a false saved connection on screen", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?scenario=client-saved");
  await page.evaluate(() => window.careTest.failNext("ClearRole", "private native failure"));
  await page.getByRole("button", { name: "Disconnect", exact: true }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Disconnect", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Find your clinic's server" })).toBeVisible();
  await expect(page.getByLabel("Clinic address")).toBeFocused();
  await expect(page.getByRole("button", { name: "Open CARE", exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Disconnect", exact: true })).toHaveCount(0);
  expect(await page.evaluate(() => window.careTest.state.client_url)).toBe("");
  await page.getByRole("button", { name: "Back", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Set up CARE on this computer" })).toBeVisible();
});

test("disconnect confirms, survives permission failure, and never deletes clinic data", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?scenario=client-saved");
  await page.getByRole("button", { name: "Disconnect", exact: true }).click();
  const dialog = page.getByRole("alertdialog");
  await expect(dialog).toContainText("No patient or clinic data is deleted");
  expect(await calls(page, "DisconnectClient")).toBe(0);
  await page.evaluate(() => window.careTest.failNext("DisconnectClient", "could not remove the clinic certificate: permission denied"));
  await dialog.getByRole("button", { name: "Disconnect", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("Couldn't disconnect this computer");
  expect(await page.evaluate(() => window.careTest.state.client_url)).toBe("https://care.local");
  await dialog.getByRole("button", { name: "Disconnect", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Set up CARE on this computer" })).toBeVisible();
  for (const method of ["PurgeResidue", "RunUninstall", "RemoveApp"]) expect(await calls(page, method)).toBe(0);
});

test("a partially saved connection stays removable without claiming connection success", async ({ page }) => {
  await client(page);
  await page.getByRole("button", { name: "Find server" }).click();
  await page.evaluate(() => {
    window.careTest.state.role = "client";
    window.careTest.state.client_url = "https://care.local";
    window.careTest.failNext("ConnectClient", "could not install the clinic certificate: permission denied");
  });
  await page.getByRole("button", { name: "Connect", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Your computer didn't allow the connection");
  await expect(page.getByRole("heading", { name: "You're connected" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Disconnect", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: "Back", exact: true }).click();
  await expect(page.getByRole("button", { name: "Disconnect", exact: true })).toBeVisible();
  expect(await page.evaluate(() => window.careTest.state.role)).toBe("client");
});

test("space blocks setup before any downloads or configuration writes", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?scenario=setup-space");
  await expect(page.getByText("Only 12 GB is free. CARE needs 30 GB.")).toBeVisible();
  await expect(forward(page)).toBeDisabled();
  for (const method of ["InstallDocker", "InstallGit", "PurgeResidue", "SetMDNSName", "RunSetup"]) expect(await calls(page, method)).toBe(0);
  await page.evaluate(() => { window.careTest.fixtures.disk.ok = true; });
  await page.getByRole("button", { name: "Check again" }).click();
  await expect(page.getByRole("heading", { name: "Choosing the clinic address" })).toBeVisible();
});

test("platform-inapplicable steps are omitted without skipping unknown failures", async ({ page }) => {
  await address(page);
  await expect(page.getByRole("complementary", { name: "Setup progress" })).toContainText("Step 3 of 7");
  await expect(page.getByRole("complementary", { name: "Setup progress" })).not.toContainText("Windows setup");
  await address(page, "windows");
  await expect(page.getByRole("complementary", { name: "Setup progress" })).toContainText("Step 6 of 10");
  await start(page);
  await page.evaluate(() => window.careTest.failNext("WSLStatus", "private WSL probe failed"));
  await page.getByRole("button", { name: "Start setup" }).click();
  await expect(page.getByRole("heading", { name: "Choosing the clinic address" })).toBeVisible();
  expect(await calls(page, "WSLStatus")).toBeGreaterThanOrEqual(2);
});

for (const missing of ["docker", "git"] as const) {
  test(`required software shows missing ${missing} in red and ready software in green`, async ({ page }) => {
    await start(page);
    await page.evaluate((id) => { window.careTest.fixtures[id].ok = false; }, missing);
    await page.getByRole("button", { name: "Start setup" }).click();
    await expect(page.getByRole("heading", { name: "Installing what CARE needs" })).toBeVisible();
    const rows = page.locator(".on-data-row");
    const incomplete = rows.filter({ hasText: "Action required" });
    const ready = rows.filter({ has: page.getByText("Ready", { exact: true }) });
    await expect(incomplete).toHaveCount(1);
    await expect(ready).toHaveCount(1);
    for (const selector of [".on-badge", ".on-tile"]) {
      await expect(incomplete.locator(selector)).toHaveCSS("background-color", "rgb(253, 236, 236)");
      await expect(incomplete.locator(selector)).toHaveCSS("color", "rgb(153, 27, 27)");
      await expect(ready.locator(selector)).toHaveCSS("background-color", "rgb(227, 247, 238)");
      await expect(ready.locator(selector)).toHaveCSS("color", "rgb(4, 108, 78)");
    }
    await expect(forward(page)).toBeDisabled();
  });
}

test("software downloads wait for a real size and remain single-flight", async ({ page }) => {
  await start(page);
  await page.evaluate(() => {
    window.careTest.fixtures.docker.ok = false;
    window.careTest.fixtures.git.ok = false;
    window.careTest.hold("RancherDownloadInfo");
    window.careTest.hold("InstallDocker");
  });
  await page.getByRole("button", { name: "Start setup" }).click();
  await expect(page.getByRole("heading", { name: "Installing what CARE needs" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Install them" })).toBeDisabled();
  await page.evaluate(() => window.careTest.release("RancherDownloadInfo"));
  await page.getByRole("button", { name: "Install them" }).evaluate((button: HTMLButtonElement) => { button.click(); button.click(); });
  await expect.poll(() => calls(page, "InstallDocker")).toBe(1);
  await expect(page.getByRole("button", { name: "Back", exact: true })).toBeDisabled();
  await expect(forward(page)).toBeDisabled();
  await expect(page.locator(".on-data-row .on-badge")).toHaveText(["Working", "Waiting"]);
  await expect(page.locator(".on-data-row .on-bad")).toHaveCount(0);
  await page.evaluate(() => window.careTest.emit("prereq-download-progress", { name: "Rancher Desktop", phase: "downloading", done: 2e6, total: 0 }));
  await expect(page.getByRole("progressbar", { name: "Rancher Desktop download progress" })).not.toHaveAttribute("aria-valuenow");
  await page.evaluate(() => window.careTest.release("InstallDocker"));
  await expect(page.getByRole("heading", { name: "Choosing the clinic address" })).toBeVisible();
  expect(await calls(page, "InstallGit")).toBe(1);
  expect(await calls(page, "RunSetup")).toBe(0);
});

for (const [platform, tool, method] of [
  ["darwin", "docker", "InstallDocker"],
  ["windows", "docker", "InstallDocker"],
  ["windows", "git", "InstallGit"],
] as const) {
  test(`${platform} ${tool} download interruption explains reconnecting and retries without resetting setup`, async ({ page }) => {
    await start(page, `&platform=${platform}`);
    await page.evaluate(({ tool, method }) => {
      window.careTest.fixtures[tool].ok = false;
      window.careTest.hold(method);
      window.careTest.failNext(method, 'could not download installer: download connection interrupted: Get "https://private.example/installer": unexpected EOF');
    }, { tool, method });
    await page.getByRole("button", { name: "Start setup" }).click();
    await page.getByRole("button", { name: tool === "docker" ? "Install them" : "Install Git", exact: true }).click();
    await expect.poll(() => calls(page, method)).toBe(1);
    await page.evaluate(() => window.careTest.emit("prereq-download-progress", {
      name: "installer", phase: "downloading", done: 12e6, total: 612e6,
    }));
    await expect(page.getByRole("progressbar")).toBeVisible();
    await page.evaluate((method) => {
      window.careTest.emit("prereq-download-progress", { name: "installer", phase: "failed", done: 12e6, total: 612e6 });
      window.careTest.release(method);
    }, method);
    const failure = page.getByRole("alert").filter({ hasText: "The download was interrupted" });
    await expect(failure).toBeVisible();
    await expect(failure).toContainText("Check the internet connection, then try again.");
    await expect(failure).toContainText("downloads the file from the beginning");
    await expect(failure).not.toContainText(/private\.example|unexpected EOF|didn't approve|The required change couldn't finish/);
    await expect(forward(page)).toBeDisabled();
    await expect(page.locator(".on-data-row .on-badge").filter({ hasText: "Action required" })).toHaveCount(1);
    await page.setViewportSize({ width: 720, height: 560 });
    await fits(page);
    await page.evaluate((method) => window.careTest.hold(method), method);
    await failure.getByRole("button", { name: "Try again", exact: true }).evaluate((button: HTMLButtonElement) => {
      button.click(); button.click();
    });
    await expect.poll(() => calls(page, method)).toBe(2);
    await expect(forward(page)).toBeDisabled();
    await page.evaluate((method) => window.careTest.release(method), method);
    await expect(page.getByRole("heading", { name: "Choosing the clinic address", exact: true })).toBeVisible();
    for (const action of ["RunSetup", "CleanupFailedInstall", "PurgeResidue"]) expect(await calls(page, action)).toBe(0);
    expect(await page.evaluate(() => window.careTest.logs.some((line) => line.includes("unexpected EOF")))).toBe(true);
  });
}

test("an offline Rancher size preview asks to reconnect before any download", async ({ page }) => {
  await start(page);
  await page.evaluate(() => {
    window.careTest.fixtures.docker.ok = false;
    window.careTest.failNext("RancherDownloadInfo", "could not check the download size: download connection interrupted: lookup private.example: no such host");
  });
  await page.getByRole("button", { name: "Start setup" }).click();
  const failure = page.getByRole("alert").filter({ hasText: "Couldn't reach the download server" });
  await expect(failure).toBeVisible();
  await expect(failure).toContainText("Check the internet connection, then try again.");
  await expect(failure).toContainText("Nothing has been downloaded yet.");
  await expect(page.getByRole("button", { name: "Install them", exact: true })).toBeDisabled();
  expect(await calls(page, "InstallDocker")).toBe(0);
  await failure.getByRole("button", { name: "Try again", exact: true }).click();
  await expect(page.getByRole("button", { name: "Install them", exact: true })).toBeEnabled();
  await expect(failure).toHaveCount(0);
  expect(await calls(page, "InstallDocker")).toBe(0);
});

test("download guidance distinguishes connection loss, integrity and unrelated failures", () => {
  for (const cause of [
    "could not download installer: download connection interrupted: unexpected EOF",
    "could not download installer: download connection interrupted: context canceled",
    "the download of installer stopped making progress for 2m0s",
    "download timed out",
  ]) {
    expect(downloadProblem(new Error(cause))?.title).toBe("The download was interrupted");
    expect(downloadProblem(new Error(cause))?.message).toContain("downloads the file from the beginning");
    expect(downloadProblem(cause, true)?.message).toContain("Nothing has been downloaded yet.");
  }
  expect(downloadProblem("expected SHA-256 private-hash, got private-hash")).toEqual({
    title: "The downloaded file couldn't be verified",
    message: "The downloaded file isn't the one this version of CARE expects. It wasn't installed. Try again; if it happens twice, share the log file.",
  });
  for (const cause of [
    "User cancelled the authorization dialog (-128)",
    "write /private/installer: no space left on device",
    "could not download installer: the server said 403 Forbidden",
    "could not download installer: x509: certificate signed by unknown authority",
    "Rancher Desktop is installed but didn't start",
  ]) {
    expect(downloadProblem(cause)).toBeNull();
  }
});

for (const [detail, message] of [
  ["expected SHA-256 private-hash, got wrong-hash", "The downloaded file isn't the one this version of CARE expects."],
  ["User cancelled the authorization dialog (-128)", "Your computer didn't approve the change."],
  ["write /private/installer: no space left on device", "The required change couldn't finish."],
] as const) {
  test(`non-network software failure keeps its guidance: ${message}`, async ({ page }) => {
    await start(page);
    await page.evaluate((error) => {
      window.careTest.fixtures.docker.ok = false;
      window.careTest.failNext("InstallDocker", error);
    }, detail);
    await page.getByRole("button", { name: "Start setup" }).click();
    await page.getByRole("button", { name: "Install them", exact: true }).click();
    const failure = page.getByRole("alert").filter({ hasText: message });
    await expect(failure).toBeVisible();
    await expect(failure).not.toContainText(/The download was interrupted|private-hash|wrong-hash|\/private\/installer/);
    await expect(failure.getByRole("button", { name: "Try again", exact: true })).toBeEnabled();
    await expect(forward(page)).toBeDisabled();
    expect(await calls(page, "InstallDocker")).toBe(1);
  });
}

test("cleanup requires the destructive button and verifies partial removal before retry", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?scenario=setup-cleanup");
  await expect(page.getByRole("heading", { name: "Removing stale files from an earlier setup" })).toBeVisible();
  expect(await calls(page, "PurgeResidue")).toBe(0);
  await page.evaluate(() => {
    window.careTest.hold("PurgeResidue");
    window.careTest.failNext("PurgeResidue", "private permission failure");
  });
  await page.getByRole("button", { name: "Remove it all" }).click();
  await expect(page.getByRole("button", { name: "Back", exact: true })).toBeDisabled();
  await page.evaluate(() => {
    window.careTest.fixtures.residue.traces = [{ id: "hosts", label: "The old clinic address and its certificate", detail: "" }];
    window.careTest.release("PurgeResidue");
  });
  await expect(page.getByText("Containers and volumes from the earlier clinic")).toContainText("Removed");
  await expect(page.getByRole("alert")).toContainText("Cleanup didn't finish");
  expect(await page.evaluate(() => window.careTest.calls.find((call) => call.method === "PurgeResidue")?.args)).toEqual([true]);
  await page.getByRole("button", { name: "Try again", exact: true }).first().click();
  await expect(page.getByRole("heading", { name: "Choosing the clinic address" })).toBeVisible();
});

test("Windows restart can be deferred without bypassing the requirement", async ({ page }) => {
  await start(page, "&platform=windows");
  await page.evaluate(() => { window.careTest.fixtures.restart.needed = true; });
  await page.getByRole("button", { name: "Start setup" }).click();
  await expect(page.getByRole("alertdialog")).toBeVisible();
  await page.getByRole("button", { name: "I'll restart later" }).click();
  await expect(page.getByRole("heading", { name: "Getting Windows ready" })).toBeVisible();
  await expect(forward(page)).toBeDisabled();
  expect(await calls(page, "RestartNow")).toBe(0);
  await page.getByRole("button", { name: "Restart now", exact: true }).click();
  await page.evaluate(() => {
    window.careTest.hold("RestartNow");
    window.careTest.failNext("RestartNow", "private native error -128");
  });
  const dialog = page.getByRole("alertdialog");
  await dialog.getByRole("button", { name: "Restart now", exact: true }).evaluate((button: HTMLButtonElement) => {
    button.click();
    button.click();
  });
  await expect.poll(() => calls(page, "RestartNow")).toBe(1);
  await expect(dialog.getByRole("button", { name: "Restarting…", exact: true })).toBeDisabled();
  await expect(dialog.getByRole("button", { name: "I'll restart later" })).toBeDisabled();
  await page.evaluate(() => window.careTest.release("RestartNow"));
  await expect(dialog).toContainText("CARE couldn't restart this computer");
  await expect(dialog.getByRole("button", { name: "Restart now", exact: true })).toBeEnabled();
  await expect(page.getByText(/private native|-128/)).toHaveCount(0);
  await page.evaluate(() => window.careTest.failNext("RestartNow", "restart retry failed"));
  await dialog.getByRole("button", { name: "Restart now", exact: true }).click();
  await expect.poll(() => calls(page, "RestartNow")).toBe(2);
  await expect(dialog.getByRole("button", { name: "I'll restart later" })).toBeEnabled();
});

for (const lock of ["disabled", "callback"] as const) {
  test(`Restart dialog ${lock} lock keeps Later available`, async ({ page }) => {
    await mountRestartDialog(page, lock === "disabled", lock === "callback");
    const dialog = page.getByRole("alertdialog");
    await expect(dialog.getByRole("button", { name: "Restart now", exact: true })).toBeDisabled();
    await expect(dialog.getByRole("button", { name: "I'll restart later" })).toBeEnabled();
    await dialog.getByRole("button", { name: "I'll restart later" }).click();
    expect(await page.evaluate(() => window.restartDialogTest.dismissals)).toBe(1);
    expect(await calls(page, "RestartNow")).toBe(0);
  });
}

test("Restart dialog live callback blocks native invocation before a rerender", async ({ page }) => {
  await mountRestartDialog(page);
  const dialog = page.getByRole("alertdialog");
  const restart = dialog.getByRole("button", { name: "Restart now", exact: true });
  await expect(restart).toBeEnabled();
  await page.evaluate(() => window.careTest.failNext("RestartNow", "test restart failure"));
  await restart.click();
  await expect(dialog).toContainText("CARE couldn't restart this computer");
  await expect(restart).toBeEnabled();
  await restart.evaluate((button: HTMLButtonElement) => {
    window.restartDialogTest.blocked = true;
    button.click();
  });
  expect(await calls(page, "RestartNow")).toBe(1);
  await expect(dialog.getByRole("button", { name: "I'll restart later" })).toBeEnabled();
  await dialog.getByRole("button", { name: "I'll restart later" }).click();
  expect(await page.evaluate(() => window.restartDialogTest.dismissals)).toBe(1);
});

for (const item of [
  { method: "InstallWSL", button: "Install WSL 2", heading: "Getting Windows ready", fixture: "wsl" },
  { method: "InstallDocker", button: "Install them", heading: "Installing what CARE needs", fixture: "docker" },
  { method: "FixNetwork", button: "Set to Private", heading: "Setting this network to Private", fixture: "network" },
] as const) {
  test(`requirement Retry reruns ${item.method}, rather than just checking it`, async ({ page }) => {
    await start(page, "&platform=windows");
    await page.evaluate(({ method, fixture }) => {
      window.careTest.fixtures[fixture].ok = false;
      window.careTest.failNext(method, "private permission denied");
    }, item);
    await page.getByRole("button", { name: "Start setup" }).click();
    await expect(page.getByRole("heading", { name: item.heading })).toBeVisible();
    await expect(page.getByText("Action required", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: item.button, exact: true }).click();
    await expect(page.getByRole("alert")).toContainText("Your computer didn't approve the change");
    expect(await calls(page, item.method)).toBe(1);
    await page.getByRole("alert").getByRole("button", { name: "Try again", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Choosing the clinic address" })).toBeVisible();
    expect(await calls(page, item.method)).toBe(2);
    expect(await calls(page, "RunSetup")).toBe(0);
    expect(await page.locator("body").innerText()).not.toContain("private permission denied");
  });
}

test("an address field disabled during entry receives focus after its asynchronous check", async ({ page }) => {
  await start(page);
  await page.evaluate(() => window.careTest.hold("MDNSStatus"));
  await page.getByRole("button", { name: "Start setup" }).click();
  const input = page.getByLabel("Clinic address", { exact: true });
  await expect(input).toBeVisible();
  await expect(input).toBeDisabled();
  await page.evaluate(() => window.careTest.release("MDNSStatus"));
  await expect(input).toBeEnabled();
  await expect(input).toBeFocused();
});

test("address checks keep focus, discard stale answers, and never save a taken name", async ({ page }) => {
  await address(page);
  const input = page.getByLabel("Clinic address", { exact: true });
  await expect(input).toBeFocused();
  const count = await calls(page, "SetMDNSName");
  await page.evaluate(() => { window.careTest.fixtures.addressTaken = true; });
  await input.fill("care-hospital");
  await expect(page.getByText("care-hospital.local is already taken on this network — type a different name.")).toBeVisible();
  await expect(input).toBeFocused();
  await expect(forward(page)).toBeDisabled();
  expect(await calls(page, "SetMDNSName")).toBe(count);
  await page.evaluate(() => {
    window.careTest.fixtures.addressTaken = false;
    window.careTest.hold("MDNSStatus");
  });
  await input.fill("first");
  await expect.poll(() => calls(page, "MDNSStatus")).toBeGreaterThan(2);
  await input.fill("second");
  await page.waitForTimeout(400);
  await page.evaluate(() => window.careTest.release("MDNSStatus"));
  await expect(forward(page)).toBeEnabled();
  expect(await page.evaluate(() => window.careTest.state.mdns_name)).toBe("second.local");
  expect(await page.evaluate(() => window.careTest.calls.filter((call) => call.method === "SetMDNSName").some((call) => call.args[0] === "first.local"))).toBe(false);
});

test("address saves are ordered, so an older in-flight save cannot overwrite the new name", async ({ page }) => {
  await address(page);
  const baseline = await calls(page, "SetMDNSName");
  const checked = await calls(page, "MDNSStatus");
  await page.evaluate(() => window.careTest.hold("SetMDNSName"));
  await page.getByLabel("Clinic address", { exact: true }).fill("first");
  await expect.poll(() => calls(page, "SetMDNSName")).toBe(baseline + 1);
  await page.getByLabel("Clinic address", { exact: true }).fill("second");
  await expect.poll(() => calls(page, "MDNSStatus")).toBe(checked + 2);
  expect(await calls(page, "SetMDNSName")).toBe(baseline + 1);
  await expect(forward(page)).toBeDisabled();
  await page.evaluate(() => window.careTest.release("SetMDNSName"));
  await expect(forward(page)).toBeEnabled();
  expect(await calls(page, "SetMDNSName")).toBe(baseline + 2);
  expect(await page.evaluate(() => window.careTest.state.mdns_name)).toBe("second.local");
});

test("address check failures offer a log without putting technical causes on screen", async ({ page }) => {
  await address(page);
  await page.evaluate(() => window.careTest.failNext("MDNSStatus", "private multicast lookup failed"));
  await page.getByLabel("Clinic address", { exact: true }).fill("care-hospital");
  await expect(page.getByText("Couldn't check or save this clinic address. Try again, or share the log file with your support contact.")).toBeVisible();
  await expect(forward(page)).toBeDisabled();
  await page.getByRole("button", { name: "Open log file", exact: true }).click();
  expect(await calls(page, "OpenLogFolder")).toBe(1);
  expect(await page.locator("body").innerText()).not.toContain("private multicast lookup failed");
  await page.getByRole("button", { name: "Check again", exact: true }).click();
  await expect(forward(page)).toBeEnabled();
});

test("typing an address blocks an update even before the debounced check starts", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?scenario=available&role=server");
  await expect(forward(page)).toBeEnabled();
  await page.evaluate(() => {
    const input = document.querySelector<HTMLInputElement>("#mdnsname")!;
    const update = [...document.querySelectorAll("button")].find((item) => item.textContent?.trim() === "Update now")!;
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input, "care-hospital");
    input.dispatchEvent(new Event("input", { bubbles: true }));
    update.click();
  });
  await expect(forward(page)).toBeEnabled();
  expect(await calls(page, "InstallAppUpdate")).toBe(0);
  expect(await page.evaluate(() => window.careTest.state.mdns_name)).toBe("care-hospital.local");
});

test("a synchronous invalid-address retry releases the update lock", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?scenario=available&role=server");
  await expect(forward(page)).toBeEnabled();
  await page.getByLabel("Clinic address", { exact: true }).fill("-");
  await page.getByRole("button", { name: "Check again", exact: true }).click();
  await expect(page.getByRole("button", { name: "Update now", exact: true })).toBeEnabled();
  await expect(page.getByLabel("Clinic address", { exact: true })).toBeEnabled();
  await expect(forward(page)).toBeDisabled();
});

for (const [raw, expected] of [
  ["That drive is read-only: NTFS technical cause", "That location is read-only"],
  ["That is a file, not a folder.", "That's a file, not a folder"],
  ["This computer isn't allowed to write to that folder.", "This computer isn't allowed to write there"],
  ["That folder already holds backups from another CARE installation.", "backups from a different clinic"],
  ["That folder isn't there any more.", "The backup location isn't available any more"],
]) {
  test(`backup validation: ${expected}`, async ({ page }) => {
    await address(page);
    await page.evaluate((message) => { window.careTest.fixtures.folderProblem = message; }, raw);
    await forward(page).click();
    await expect(page.getByRole("alert").first()).toContainText(expected);
    await expect(page.getByRole("button", { name: "Choose where to save" })).toBeDisabled();
    await expect(forward(page)).toBeDisabled();
    await expect(page.getByText(/NTFS|USB drive|same drive/)).toHaveCount(0);
    await page.evaluate(() => { window.careTest.fixtures.folderProblem = ""; });
    await page.getByRole("button", { name: "Change folder" }).click();
    await expect(page.getByRole("button", { name: "Choose where to save" })).toBeEnabled();
  });
}

test("native dialog cancellation never marks recovery files saved or checked", async ({ page }) => {
  await backups(page);
  await page.evaluate(() => window.careTest.respond("SaveSetupBackupRecovery", false));
  await page.getByRole("button", { name: "Choose where to save" }).click();
  await expect(page.getByRole("button", { name: "Select saved file" })).toBeDisabled();
  await expect(forward(page)).toBeDisabled();
  expect(await page.evaluate(() => window.careTest.fixtures.recovery.backup_saved)).toBe(false);
});

for (const first of ["save", "update"] as const) {
  test(`recovery dialogs and app updates cannot race when ${first} starts first`, async ({ page }) => {
    await page.goto("/tests/fixtures/index.html?scenario=available&role=server");
    await expect(forward(page)).toBeEnabled();
    await forward(page).click();
    await expect(page.getByRole("button", { name: "Choose where to save", exact: true })).toBeEnabled();
    await page.evaluate((action) => {
      window.careTest.hold("SaveSetupBackupRecovery");
      const button = (text: string) => [...document.querySelectorAll("button")].find((item) => item.textContent?.trim() === text)!;
      const save = button("Choose where to save");
      const update = button("Update now");
      if (action === "save") { save.click(); update.click(); }
      else { update.click(); save.click(); }
    }, first);
    if (first === "save") {
      await expect.poll(() => calls(page, "SaveSetupBackupRecovery")).toBe(1);
      expect(await calls(page, "InstallAppUpdate")).toBe(0);
      await page.evaluate(() => window.careTest.release("SaveSetupBackupRecovery"));
      await expect(page.getByText("Recovery file saved", { exact: true })).toBeVisible();
    } else {
      await expect(page.getByText("Downloading CARE Desktop 0.1.6…", { exact: true })).toBeVisible();
      expect(await calls(page, "SaveSetupBackupRecovery")).toBe(0);
    }
    await expect(forward(page)).toBeDisabled();
  });
}

test("backup recovery is gated on the correct file, with visible real saved paths", async ({ page }) => {
  await backups(page);
  await page.getByRole("button", { name: "Choose where to save" }).click();
  await expect(page.getByText("/test-fixtures/recovery/CARE-backup-recovery.pem", { exact: true })).toBeVisible();
  await expect(forward(page)).toBeDisabled();
  await page.evaluate(() => window.careTest.failNext("VerifySetupBackupRecovery", "the recovery file does not match this clinic"));
  await page.getByRole("button", { name: "Select saved file" }).click();
  await expect(page.getByText(/That file doesn't match this clinic/)).toBeVisible();
  await expect(forward(page)).toBeDisabled();
  await page.getByRole("button", { name: "Select saved file" }).click();
  await expect(forward(page)).toBeEnabled();
});

test("a lost private recovery file requires explicit replacement and verification", async ({ page }) => {
  await address(page);
  await page.evaluate(() => {
    window.careTest.fixtures.recovery.backup_saved = true;
    window.careTest.fixtures.recovery.backup_problem = "missing";
    window.careTest.fixtures.recovery.backup_path = "/test-fixtures/lost.pem";
  });
  await forward(page).click();
  await page.getByRole("button", { name: "Save a new recovery file" }).click();
  await expect(page.getByText("/test-fixtures/recovery/CARE-backup-recovery-new.pem", { exact: true })).toBeVisible();
  await expect(forward(page)).toBeDisabled();
  await page.getByRole("button", { name: "Select saved file" }).click();
  await expect(forward(page)).toBeEnabled();
  expect(await calls(page, "ReplaceSetupBackupRecovery")).toBe(1);
});

test("password validity resets immediately when input changes and stale validation cannot pass", async ({ page }) => {
  await admin(page);
  await fillPassword(page);
  const save = page.getByRole("button", { name: "Choose where to save" });
  await expect(save).toBeEnabled();
  await page.evaluate(() => window.careTest.hold("ValidatePassword"));
  await fillPassword(page, "weak");
  await expect(save).toBeDisabled();
  await expect(forward(page)).toBeDisabled();
  await page.waitForTimeout(250);
  await page.evaluate(() => window.careTest.release("ValidatePassword"));
  await expect(page.getByText("Password must be at least 8 characters.")).toBeVisible();
  expect(await calls(page, "SaveAdminRecoveryCodes")).toBe(0);
});

test("password validator failures never log submitted credentials or host secrets", async ({ page }) => {
  await admin(page);
  const privateError = `Validation failed for ${samplePassword}; recovery=preview-recovery-code; API_TOKEN=preview-secret`;
  await page.evaluate((reason) => window.careTest.failNext("ValidatePassword", reason), privateError);
  await fillPassword(page);
  await expect(page.getByText("Couldn't check the password. Try typing it again.", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Choose where to save", exact: true })).toBeDisabled();
  await expect(forward(page)).toBeDisabled();
  const logs = await page.evaluate(() => window.careTest.logs);
  expect(logs).toContain("password validation failed");
  for (const secret of [samplePassword, "preview-recovery-code", "preview-secret"]) {
    expect(logs.join("\n")).not.toContain(secret);
  }
  expect(await page.locator("body").innerText()).not.toContain(privateError);
  await fillPassword(page, "UpdatedClinic123");
  await expect(page.getByRole("button", { name: "Choose where to save", exact: true })).toBeEnabled();
});

test("Unicode password length follows code points, and saving codes is not password storage", async ({ page }) => {
  await admin(page);
  await fillPassword(page, "Aé😀bc12x");
  await expect(page.getByRole("button", { name: "Choose where to save" })).toBeEnabled();
  await page.getByRole("button", { name: "Choose where to save" }).click();
  expect(await page.evaluate(() => window.careTest.calls.find((call) => call.method === "SaveAdminRecoveryCodes")?.args)).toEqual(["", ""]);
  await page.getByRole("button", { name: "Open to print" }).click();
  expect(await calls(page, "OpenSetupRecoveryCodes")).toBe(1);
  expect(await calls(page, "RunSetup")).toBe(0);
});

test("a folder failure on Admin is visible and leads back to Backups without losing the password", async ({ page }) => {
  await admin(page);
  await expect(page.getByLabel("Password", { exact: true })).toBeFocused();
  await fillPassword(page);
  await page.evaluate(() => { window.careTest.fixtures.folderProblem = "That folder isn't there any more."; });
  await page.getByRole("button", { name: "Choose where to save", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("The backup location needs another look");
  await expect(page.getByRole("alert")).toContainText("choose a different location");
  await expect(forward(page)).toBeDisabled();
  expect(await calls(page, "SaveAdminRecoveryCodes")).toBe(0);
  await page.getByRole("button", { name: "Check backup location", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Setting up backups" })).toBeVisible();
  await page.evaluate(() => { window.careTest.fixtures.folderProblem = ""; });
  await page.getByRole("button", { name: "Check again", exact: true }).first().click();
  await expect(forward(page)).toBeEnabled();
  await forward(page).click();
  await expect(page.getByLabel("Password", { exact: true })).toHaveValue(samplePassword);
  await expect(page.getByLabel("Confirm password", { exact: true })).toHaveValue(samplePassword);
  await page.getByRole("button", { name: "Choose where to save", exact: true }).click();
  await expect(forward(page)).toBeEnabled();
});

for (const action of ["save", "continue"] as const) {
  test(`correcting a rejected password clears the ${action} gate without losing recovery material`, async ({ page }) => {
    await admin(page);
    await fillPassword(page);
    const save = page.getByRole("button", { name: "Choose where to save", exact: true });
    await expect(save).toBeEnabled();
    if (action === "continue") {
      await save.click();
      await expect(forward(page)).toBeEnabled();
    }
    await page.evaluate(() => window.careTest.respond("ValidatePassword", "Password validation rejected this value."));
    await (action === "save" ? save : forward(page)).click();
    await expect(page.getByText(/Choose and confirm a valid password before/)).toBeVisible();
    await expect(forward(page)).toBeDisabled();
    await page.evaluate(() => window.careTest.respond("ValidatePassword", ""));
    await fillPassword(page, "UpdatedClinic123");
    await expect(page.getByText(/Choose and confirm a valid password before/)).toHaveCount(0);
    if (action === "save") await save.click();
    await expect(forward(page)).toBeEnabled();
    expect(await calls(page, "SaveAdminRecoveryCodes")).toBe(1);
    expect(await page.evaluate(() => window.careTest.logs.some((line) => /ClinicTest123|UpdatedClinic123/.test(line)))).toBe(false);
  });
}

for (const raw of [
  "keep recovery materials outside CARE's settings, installation, logs and backup folder: private path",
  "could not save the recovery file; choose a new filename: file exists",
]) {
  test(`admin recovery save failures preserve the gate: ${raw.slice(0, 30)}`, async ({ page }) => {
    await admin(page);
    await fillPassword(page);
    await page.evaluate((message) => window.careTest.failNext("SaveAdminRecoveryCodes", message), raw);
    await page.getByRole("button", { name: "Choose where to save" }).click();
    await expect(page.getByRole("alert")).toContainText("The recovery codes couldn't be saved or checked");
    await expect(forward(page)).toBeDisabled();
    expect(await page.locator("body").innerText()).not.toContain(raw);
    await page.getByRole("button", { name: "Choose where to save" }).click();
    await expect(forward(page)).toBeEnabled();
  });
}

for (const step of ["space", "windows", "software", "cleanup", "network", "address", "backup", "admin"] as const) {
  test(`Review revalidates ${step} before RunSetup and stays on Review`, async ({ page }) => {
    await review(page, step === "windows" || step === "network" ? "windows" : "darwin");
    await page.evaluate((target: SetupPage) => {
      const f = window.careTest.fixtures;
      if (target === "space") f.disk.ok = false;
      if (target === "windows") f.restart.needed = true;
      if (target === "software") f.docker.ok = false;
      if (target === "cleanup") f.residue = { clean: false, traces: [{ id: "volumes", label: "Earlier clinic volumes", detail: "" }] };
      if (target === "network") f.network.ok = false;
      if (target === "address") f.addressTaken = true;
      if (target === "backup") f.folderProblem = "That folder isn't there any more.";
      if (target === "admin") f.recovery.codes_problem = "missing";
    }, step);
    await install(page).click();
    await expect(page.getByRole("heading", { name: "Review before installing" })).toBeVisible();
    await expect(page.getByRole("alert")).toContainText("One step needs another look");
    await expect(install(page)).toBeDisabled();
    expect(await calls(page, "RunSetup")).toBe(0);
    await expect(page.getByRole("button", { name: "Back", exact: true })).toHaveCount(0);
  });
}

test("Review Fix returns directly and preserves address, password and recovery decisions", async ({ page }) => {
  await review(page);
  const savedCodes = await calls(page, "SaveAdminRecoveryCodes");
  await page.evaluate(() => {
    window.careTest.fixtures.docker.ok = false;
    window.careTest.respond("DockerPlan", { action: "open", label: "Open Rancher Desktop", detail: "", url: "", download_preview: false });
  });
  await install(page).click();
  await page.getByRole("button", { name: "Fix Required software" }).click();
  await expect(page.getByRole("heading", { name: "Installing what CARE needs" })).toBeVisible();
  await expect(page.locator('[aria-current="step"]')).toContainText("Review");
  await page.getByRole("button", { name: "Start Rancher Desktop" }).click();
  await expect(page.getByRole("heading", { name: "Review before installing" })).toBeVisible();
  await expect(install(page)).toBeEnabled();
  expect(await calls(page, "SaveAdminRecoveryCodes")).toBe(savedCodes);
  await page.getByRole("button", { name: "Edit Admin login" }).click();
  await expect(page.getByLabel("Password", { exact: true })).toHaveValue(samplePassword);
  await expect(page.getByLabel("Confirm password", { exact: true })).toHaveValue(samplePassword);
  await page.getByRole("button", { name: "Back to review", exact: true }).first().click();
  await expect(install(page)).toBeEnabled();
});

test("editing Admin focuses the password after the saved-file check finishes", async ({ page }) => {
  await review(page);
  await page.evaluate(() => window.careTest.hold("GetSetupRecoveryStatus"));
  await page.getByRole("button", { name: "Edit Admin login" }).click();
  const input = page.getByLabel("Password", { exact: true });
  await expect(input).toBeVisible();
  await expect(input).toBeDisabled();
  await page.evaluate(() => window.careTest.release("GetSetupRecoveryStatus"));
  await expect(input).toBeEnabled();
  await expect(input).toBeFocused();
  await expect(input).toHaveValue(samplePassword);
});

test("RunSetup rejection before acceptance stays on Review; acceptance alone enters installation", async ({ page }) => {
  await review(page);
  await page.evaluate(() => window.careTest.failNext("RunSetup", "something else is still running - private details"));
  await install(page).click();
  await expect(page.getByRole("heading", { name: "Review before installing" })).toBeVisible();
  await expect(page.getByText("Installation hasn't started.", { exact: false })).toBeVisible();
  await page.evaluate(() => window.careTest.emit("care-done", 1, "prerequisite"));
  await expect(page.getByRole("heading", { name: "Review before installing" })).toBeVisible();
  await page.evaluate(() => {
    window.careTest.respond("RunSetup", undefined);
    window.careTest.hold("RunSetup");
  });
  await install(page).evaluate((button: HTMLButtonElement) => { button.click(); button.click(); });
  await expect.poll(() => calls(page, "RunSetup")).toBe(2);
  await expect(install(page)).toBeDisabled();
  await page.evaluate(() => window.careTest.release("RunSetup"));
  await expect(page.getByRole("heading", { name: "Installing CARE", exact: true })).toBeVisible();
  await expect(page.locator("aside")).toHaveCSS("width", "272px");
  expect(await page.evaluate(() => {
    const attempts = window.careTest.calls.filter((call) => call.method === "RunSetup");
    return attempts[attempts.length - 1]?.args;
  })).toEqual(["care.local", samplePassword, ""]);
});

for (const { platform, cleanup, total } of [
  { platform: "darwin", cleanup: false, total: 7 },
  { platform: "darwin", cleanup: true, total: 8 },
  { platform: "windows", cleanup: false, total: 10 },
]) {
  test(`installation preserves the ${total}-step ${platform} wizard rail`, async ({ page }) => {
    if (cleanup) {
      await page.goto(`/tests/fixtures/index.html?scenario=setup-cleanup&platform=${platform}`);
      await page.getByRole("button", { name: "Remove it all" }).click();
      await expect(page.getByRole("heading", { name: "Choosing the clinic address" })).toBeVisible();
      await forward(page).click();
      await saveBackup(page);
      await forward(page).click();
      await fillPassword(page);
      await page.getByRole("button", { name: "Choose where to save", exact: true }).click();
      await forward(page).click();
      await expect(page.getByRole("heading", { name: "Review before installing" })).toBeVisible();
      await expect(install(page)).toBeEnabled();
    } else {
      await review(page, platform);
    }
    const rail = page.getByRole("complementary", { name: "Setup progress" });
    const labels = rail.locator(".on-rail-step > span:nth-child(2)");
    await expect(rail).toContainText(`Step ${total - 1} of ${total}`);
    const reviewedPages = await labels.allTextContents();
    expect(reviewedPages).toHaveLength(total);
    await page.evaluate(() => window.careTest.respond("RunSetup", undefined));
    await install(page).click();
    await expect(page.getByRole("heading", { name: "Installing CARE", exact: true })).toBeVisible();
    await expect(rail).toContainText(`Step ${total} of ${total}`);
    await expect(labels).toHaveText(reviewedPages);
    await expect(rail.locator('[aria-current="step"]')).toContainText("Install");
    await capture(page, `onboarding-install-rail-${platform}-${total}`);
  });
}

for (const size of [{ width: 1100, height: 700 }, { width: 720, height: 560 }]) {
  test(`all configuration pages fit ${size.width}x${size.height}`, async ({ page }) => {
    await page.setViewportSize(size);
    await address(page);
    await fits(page); await capture(page, `onboarding-address-${size.width}x${size.height}`);
    await forward(page).click();
    await expect(page.getByRole("heading", { name: "Setting up backups" })).toBeVisible();
    await fits(page); await capture(page, `onboarding-backups-${size.width}x${size.height}`);
    await saveBackup(page);
    await forward(page).click();
    await expect(page.getByRole("heading", { name: "Creating the admin password" })).toBeVisible();
    await fits(page); await capture(page, `onboarding-admin-${size.width}x${size.height}`);
    await fillPassword(page);
    await page.getByRole("button", { name: "Choose where to save" }).click();
    await forward(page).click();
    await expect(install(page)).toBeEnabled();
    await fits(page); await capture(page, `onboarding-review-${size.width}x${size.height}`);
  });
  test(`client states fit ${size.width}x${size.height}`, async ({ page }) => {
    await page.setViewportSize(size);
    await client(page);
    await fits(page); await capture(page, `onboarding-client-entry-${size.width}x${size.height}`);
    await page.getByRole("button", { name: "Find server" }).click();
    await expect(page.getByRole("heading", { name: "We found your clinic's server" })).toBeVisible();
    await fits(page); await capture(page, `onboarding-client-found-${size.width}x${size.height}`);
    await page.evaluate(() => window.careTest.hold("ConnectClient"));
    await page.getByRole("button", { name: "Connect", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Connecting to care.local" })).toBeVisible();
    await fits(page); await capture(page, `onboarding-client-connecting-${size.width}x${size.height}`);
    await page.evaluate(() => window.careTest.emit("client-connect-progress", "checking"));
    await expect(page.getByText("Making sure CARE opens safely")).toBeVisible();
    await fits(page); await capture(page, `onboarding-client-checking-${size.width}x${size.height}`);
    await page.evaluate(() => window.careTest.release("ConnectClient"));
    await expect(page.getByRole("heading", { name: "You're connected" })).toBeVisible();
    await fits(page); await capture(page, `onboarding-client-connected-${size.width}x${size.height}`);
    await page.goto("/tests/fixtures/index.html?scenario=client-offline");
    await expect(page.getByText("This computer is set up for care.local, but we can't find it right now")).toBeVisible();
    await fits(page); await capture(page, `onboarding-client-offline-${size.width}x${size.height}`);
    await page.getByRole("button", { name: "Disconnect", exact: true }).scrollIntoViewIfNeeded();
    await expect(page.getByRole("button", { name: "Disconnect", exact: true })).toBeInViewport();
    await fits(page);
  });
  test(`requirement and taken-address screens fit ${size.width}x${size.height}`, async ({ page }) => {
    await page.setViewportSize(size);
    for (const [scenario, title, requiredActions] of [
      ["setup-space", "Room for the clinic", 1],
      ["setup-windows", "Getting Windows ready", 1],
      ["setup-software", "Installing what CARE needs", 2],
      ["setup-cleanup", "Removing stale files from an earlier setup", 0],
      ["setup-address", "Choosing the clinic address", 0],
    ] as const) {
      await page.goto(`/tests/fixtures/index.html?scenario=${scenario}`);
      await expect(page.getByRole("heading", { name: title, exact: true })).toBeVisible();
      await expect(page.getByRole("button", { name: "Back", exact: true })).toBeEnabled();
      await expect(page.getByText("Action required", { exact: true })).toHaveCount(requiredActions);
      await expect(page.getByText("Needs you", { exact: true })).toHaveCount(0);
      await fits(page); await capture(page, `onboarding-${scenario}-${size.width}x${size.height}`);
      await expect(forward(page)).toBeInViewport();
      if (scenario === "setup-windows") {
        await page.evaluate(() => { window.careTest.fixtures.wsl.ok = true; });
        await page.getByRole("button", { name: "Check again", exact: true }).click();
        await expect(page.getByRole("heading", { name: "Setting this network to Private" })).toBeVisible();
        await expect(page.getByText("Action required", { exact: true })).toBeVisible();
        await expect(forward(page)).toBeDisabled();
        await fits(page); await capture(page, `onboarding-setup-network-${size.width}x${size.height}`);
      }
    }
  });
  test(`Windows setup with the update card stays usable at ${size.width}x${size.height}`, async ({ page }) => {
    await page.setViewportSize(size);
    await page.goto("/tests/fixtures/index.html?scenario=available&role=server&platform=windows");
    await expect(forward(page)).toBeEnabled();
    await expect(page.getByRole("complementary", { name: "Setup progress" })).toContainText("Step 6 of 10");
    await fits(page); await capture(page, `onboarding-windows-address-${size.width}x${size.height}`);
    await forward(page).click();
    await expect(page.getByRole("button", { name: "Choose where to save", exact: true })).toBeEnabled();
    await fits(page); await capture(page, `onboarding-windows-backups-${size.width}x${size.height}`);
    await saveBackup(page);
    await forward(page).click();
    await expect(page.getByLabel("Password", { exact: true })).toBeFocused();
    await fits(page); await capture(page, `onboarding-windows-admin-${size.width}x${size.height}`);
    await fillPassword(page);
    await page.getByRole("button", { name: "Choose where to save", exact: true }).click();
    await expect(forward(page)).toBeEnabled();
    await page.getByRole("button", { name: "Save a new set", exact: true }).scrollIntoViewIfNeeded();
    await fits(page); await capture(page, `onboarding-windows-admin-saved-${size.width}x${size.height}`);
    await forward(page).click();
    await expect(install(page)).toBeEnabled();
    await fits(page); await capture(page, `onboarding-windows-review-${size.width}x${size.height}`);
    await page.getByRole("button", { name: "Edit Admin login" }).scrollIntoViewIfNeeded();
    await expect(page.getByRole("button", { name: "Edit Admin login" })).toBeInViewport();
    await page.getByRole("button", { name: "Update now", exact: true }).scrollIntoViewIfNeeded();
    await expect(page.getByRole("button", { name: "Update now", exact: true })).toBeInViewport();
    await expect(install(page)).toBeInViewport();
    await fits(page);
  });
}
