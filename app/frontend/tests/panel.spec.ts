import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { expect, test, type Page } from "@playwright/test";
import type {} from "./fixtures/host";

const calls = (page: Page, method: Parameters<Window["careTest"]["failNext"]>[0]) => page.evaluate((name) =>
  window.careTest.calls.filter((call) => call.method === name).length, method);
const overview = (page: Page) => page.locator('.panel-page[aria-label="Overview"]');
const clinic = (page: Page) => overview(page).getByRole("region", { name: "Clinic status", exact: true });
const section = (page: Page, name: string) => page.getByRole("navigation", { name: "Clinic sections" })
  .getByRole("button", { name: new RegExp(`^${name}`) });
const software = (page: Page) => page.getByRole("region", { name: "CARE software", exact: true });
const desktop = (page: Page) => page.getByRole("region", { name: "CARE Desktop application", exact: true });

async function openPanel(page: Page, scenario = "panel-current", query = "") {
  await page.goto(`/tests/fixtures/index.html?scenario=${scenario}${query}`);
  await expect(overview(page).getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
  await expect(clinic(page).getByRole("heading", { name: "Running", exact: true })).toBeVisible();
}

async function fits(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  const body = page.locator(".care-panel-content");
  expect(await body.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
}

async function capture(page: Page, name: string) {
  if (!process.env.CARE_SCREENSHOTS_DIR) return;
  await mkdir(process.env.CARE_SCREENSHOTS_DIR, { recursive: true });
  await page.evaluate(() => document.fonts.ready);
  await page.screenshot({ path: join(process.env.CARE_SCREENSHOTS_DIR, `${name}.png`), animations: "disabled" });
}

test.beforeEach(async ({ page }) => {
  await page.routeWebSocket("**", (socket) => socket.send('{"type":"connected"}'));
  page.on("pageerror", (error) => { throw error; });
});

for (const viewport of [{ width: 1100, height: 700 }, { width: 720, height: 560 }]) {
  test(`panel layouts and real QR fit ${viewport.width}x${viewport.height}`, async ({ page }) => {
    await page.setViewportSize(viewport);
    await openPanel(page);
    await expect(page.locator(".care-panel-rail")).toHaveCount(1);
    await expect(overview(page).getByRole("region", { name: "What the clinic needs" })).toHaveCount(0);
    await expect(overview(page).getByText("This computer's drive")).toHaveCount(0);
    await expect(overview(page).getByText("Admin sign-in")).toHaveCount(0);
    await fits(page);
    await capture(page, `panel-actual-overview-${viewport.width}`);

    const connect = overview(page).getByRole("button", { name: "Connect a phone or tablet", exact: true });
    await connect.click();
    const dialog = page.getByRole("alertdialog");
    await expect(dialog.getByRole("img", { name: "QR code for http://care.local/setup" })).toBeVisible();
    expect(await dialog.locator("svg[role=img] path").last().getAttribute("d")).toMatch(/M/);
    await expect(dialog.getByText("http://care.local/setup", { exact: true })).toBeVisible();
    await capture(page, `panel-actual-qr-${viewport.width}`);
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(connect).toBeFocused();

    await section(page, "Storage").click();
    await expect(page.getByRole("heading", { name: "Storage", exact: true })).toBeVisible();
    await expect(page.getByText("37 GB currently used", { exact: true })).toBeVisible();
    await fits(page);
    await capture(page, `panel-actual-storage-${viewport.width}`);
    await section(page, "Updates").click();
    await expect(software(page).getByRole("heading", { name: "CARE software" })).toBeVisible();
    await expect(desktop(page).getByRole("heading", { name: "CARE Desktop application" })).toBeVisible();
    await expect(software(page)).not.toContainText("preview-backend-current");
    await expect(software(page)).not.toContainText("branch");
    await fits(page);
    await capture(page, `panel-actual-updates-${viewport.width}`);
    await page.evaluate(() => {
      window.careTest.fixtures.channel.pending_backend = "private-update-revision";
      window.careTest.emit("care-update", { backend: "private-update-revision", frontend: "" });
      window.careTest.setUpdate({
        current: "0.1.5", version: "0.1.6", available: true,
        notes_url: "https://github.com/ohcnetwork/care_desktop/releases",
        asset: "CARE-preview.dmg", size: 50_000_000,
      });
    });
    await desktop(page).getByRole("button", { name: "Check CARE Desktop updates" }).click();
    await expect(desktop(page)).toContainText("CARE Desktop 0.1.6 is available");
    await fits(page);
    await capture(page, `panel-actual-updates-available-${viewport.width}`);
    await desktop(page).getByRole("button", { name: "Update CARE Desktop", exact: true }).scrollIntoViewIfNeeded();
    await fits(page);
    await capture(page, `panel-actual-desktop-available-${viewport.width}`);
    await section(page, "Advanced").click();
    await expect(page.getByRole("heading", { name: "Advanced", level: 1, exact: true })).toHaveCount(1);
    await expect(page.getByRole("heading", { name: "Advanced", level: 1, exact: true })).toBeInViewport();
    await fits(page);
    await capture(page, `panel-actual-advanced-shell-${viewport.width}`);
    await section(page, "Plugins").click();
    await expect(page.getByRole("heading", { name: "Plugins", level: 1, exact: true })).toHaveCount(1);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await capture(page, `panel-actual-plugins-shell-${viewport.width}`);
  });
}

test("start, stop and restart stay busy until native completion and ignore double clicks", async ({ page }) => {
  await openPanel(page);
  await page.evaluate(() => {
    window.careTest.fixtures.finishJobs = false;
    window.careTest.hold("ClinicAction");
  });
  await clinic(page).getByRole("button", { name: "Stop", exact: true }).dblclick();
  await expect.poll(() => calls(page, "ClinicAction")).toBe(1);
  await expect(clinic(page).getByRole("heading", { name: "Stopping", exact: true })).toBeVisible();
  await page.evaluate(() => window.careTest.release("ClinicAction"));
  await expect(clinic(page).getByRole("heading", { name: "Stopping", exact: true })).toBeVisible();
  await page.evaluate(() => window.careTest.finishJob("stop"));
  await expect(clinic(page).getByRole("heading", { name: "Stopped", exact: true })).toBeVisible();
  await capture(page, "panel-actual-stopped-1100");
  await expect(overview(page).getByRole("button", { name: "Open CARE", exact: true })).toBeDisabled();
  await expect(overview(page).getByRole("button", { name: "Copy", exact: true })).toBeEnabled();
  await page.evaluate(() => window.careTest.hold("ClinicAction"));
  await clinic(page).getByRole("button", { name: "Start clinic", exact: true }).dblclick();
  await expect.poll(() => calls(page, "ClinicAction")).toBe(2);
  await page.evaluate(() => window.careTest.release("ClinicAction"));
  await expect(clinic(page).getByRole("heading", { name: "Starting", exact: true })).toBeVisible();
  await page.evaluate(() => window.careTest.finishJob("start"));
  await expect(clinic(page).getByRole("heading", { name: "Running", exact: true })).toBeVisible();
});

test("native action rejection is actionable, not a success or a technical error dump", async ({ page }) => {
  await openPanel(page);
  await page.evaluate(() => window.careTest.failNext("ClinicAction", "/private/patient-path: EACCES exit status 1"));
  await clinic(page).getByRole("button", { name: "Restart", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "CARE couldn't restart" })).toBeVisible();
  await expect(page.locator(".care-panel")).not.toContainText("/private/patient-path");
  await expect(page.locator(".care-panel")).not.toContainText("exit status");
  await expect(clinic(page).getByRole("button", { name: "Restart", exact: true })).toBeEnabled();
  expect(await page.evaluate(() => window.careTest.logs.some((line) => line.includes("EACCES")))).toBe(true);
});

test("an asynchronous action failure leaves a persistent readable message", async ({ page }) => {
  await openPanel(page);
  await page.evaluate(() => { window.careTest.fixtures.finishJobs = false; });
  await clinic(page).getByRole("button", { name: "Restart", exact: true }).click();
  await expect(clinic(page).getByRole("heading", { name: "Restarting", exact: true })).toBeVisible();
  await capture(page, "panel-actual-restarting-1100");
  await page.evaluate(() => window.careTest.finishJob("restart", "docker compose: private technical reason"));
  await expect(page.getByRole("alert").filter({ hasText: "CARE couldn't restart" })).toBeVisible();
  await expect(page.locator(".care-panel")).not.toContainText("private technical reason");
  await expect(clinic(page).getByRole("heading", { name: "Running", exact: true })).toBeVisible();
  await capture(page, "panel-actual-action-error-1100");
});

test("empty backup history never claims a backup has started or succeeded", async ({ page }) => {
  await openPanel(page, "panel-no-backups");
  const summary = overview(page).getByRole("region", { name: "Backup summary" });
  await expect(summary).toContainText("No backups yet");
  await expect(summary).not.toContainText("Started");
  await expect(summary).not.toContainText("Up to date");
});

test("storage refresh and cleanup use native results without invented freed-space totals", async ({ page }) => {
  await openPanel(page);
  await section(page, "Storage").click();
  const storage = page.locator('.panel-page[aria-label="Storage"]');
  await expect(storage.getByText("37 GB currently used", { exact: true })).toBeVisible();
  const vm = storage.locator(".panel-storage-vm");
  await expect(vm.getByRole("heading", { name: "CARE Desktop storage" })).toBeVisible();
  await expect(vm).toContainText("Storage capacity: 100 GB");
  await expect(vm).toContainText("not the amount used");
  await expect(vm).toContainText("Other apps using Rancher Desktop may also be included.");
  await expect(vm.getByRole("progressbar")).toHaveCount(0);
  await page.evaluate(() => {
    window.careTest.fixtures.finishJobs = false;
    window.careTest.hold("ClinicAction");
  });
  await storage.getByRole("button", { name: "Free up space", exact: true }).dblclick();
  await expect.poll(() => calls(page, "ClinicAction")).toBe(1);
  await expect(storage.getByRole("button", { name: "Freeing up space…" })).toBeDisabled();
  await page.evaluate(() => {
    window.careTest.release("ClinicAction");
    window.careTest.fixtures.storage.drives![1].free = 70 * 2 ** 30;
    window.careTest.finishJob("free-space");
  });
  await expect(storage.getByText("29 GB currently used", { exact: true })).toBeVisible();
  await expect(storage).not.toContainText("freed");
});

test("storage read failure retains explicitly old readings and can be retried", async ({ page }) => {
  await openPanel(page);
  await page.evaluate(() => window.careTest.failNext("RecheckStorage", "stat /private/data: failed"));
  await section(page, "Storage").click();
  const storage = page.locator('.panel-page[aria-label="Storage"]');
  await expect(storage.getByRole("alert")).toContainText("last successful check");
  await expect(storage).not.toContainText("/private/data");
  await storage.getByRole("button", { name: "Check now", exact: true }).click();
  await expect(storage.getByRole("alert")).toHaveCount(0);
  await page.evaluate(() => {
    const drive = window.careTest.fixtures.storage.drives![1];
    drive.level = "unknown"; drive.free = 0; drive.total = 0;
    window.careTest.emit("care-storage", window.careTest.fixtures.storage);
  });
  const vm = storage.locator("section").filter({ has: page.getByRole("heading", { name: "CARE Desktop storage", exact: true }) });
  await expect(vm).toContainText("Space unavailable");
  await expect(vm.locator(".panel-storage-usage")).toHaveCount(0);
  await expect(vm.getByRole("progressbar")).toHaveCount(0);
});

test("requirements stay absent when healthy, recheck throughout panel lifetime, and fix once", async ({ page }) => {
  await page.clock.install();
  await openPanel(page);
  await expect.poll(() => calls(page, "GitStatus")).toBeGreaterThan(0);
  await expect(overview(page).getByRole("region", { name: "What the clinic needs" })).toHaveCount(0);
  await page.evaluate(() => {
    window.careTest.fixtures.git = { ok: false, message: "/private/git not found: exit status 127" };
  });
  await page.clock.fastForward(60_100);
  const requirements = overview(page).getByRole("region", { name: "What the clinic needs" });
  await expect(requirements).toContainText("Git needs setup");
  await expect(requirements).not.toContainText("exit status");
  await page.evaluate(() => {
    window.careTest.hold("InstallGit");
    window.careTest.respond("InstallGit", "");
  });
  await requirements.getByRole("button", { name: "Install Git", exact: true }).dblclick();
  await expect.poll(() => calls(page, "InstallGit")).toBe(1);
  await expect(section(page, "Backups")).toBeDisabled();
  await page.evaluate(() => {
    window.careTest.fixtures.git.ok = true;
    window.careTest.release("InstallGit");
  });
  await expect(requirements).toHaveCount(0);
  await expect(section(page, "Backups")).toBeEnabled();
});

test("Rancher download confirmation cancels without installation", async ({ page }) => {
  await openPanel(page, "panel-requirements");
  const requirements = overview(page).getByRole("region", { name: "What the clinic needs" });
  await expect(requirements).toBeVisible();
  await expect(requirements).toContainText("Rancher Desktop needs setup");
  await expect(requirements).toContainText("Git needs setup");
  await expect(requirements.getByText("Runs the clinic software on this computer.", { exact: true })).toBeVisible();
  await expect(requirements.getByText("Downloads the clinic software and its updates.", { exact: true })).toBeVisible();
  await expect(requirements.getByText(/^(Ready|Action required)$/)).toHaveCount(0);
  await requirements.getByRole("button", { name: "Install Rancher Desktop", exact: true }).click();
  const dialog = page.getByRole("alertdialog");
  await expect(dialog.getByRole("heading", { name: "Download Rancher Desktop?" })).toBeVisible();
  expect(await calls(page, "InstallDocker")).toBe(0);
  await dialog.getByRole("button", { name: "Not now", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(await calls(page, "InstallDocker")).toBe(0);
  await expect(requirements.getByRole("button", { name: "Install Rancher Desktop", exact: true })).toBeEnabled();
});

for (const stage of ["preview", "download"] as const) {
  test(`Rancher repair ${stage} connection loss gives reconnect guidance and remains retryable`, async ({ page }) => {
    await openPanel(page, "panel-requirements");
    const saved = await page.evaluate(() => ({
      backups: window.careTest.fixtures.backups,
      recovery: window.careTest.fixtures.recovery,
      backupDir: window.careTest.fixtures.backupDir,
    }));
    const requirements = overview(page).getByRole("region", { name: "What the clinic needs" });
    await page.evaluate((stage) => {
      window.careTest.failNext(stage === "preview" ? "RancherDownloadInfo" : "InstallDocker",
        "download connection interrupted: private.example: connection reset by peer");
    }, stage);
    await requirements.getByRole("button", { name: "Install Rancher Desktop", exact: true }).click();
    if (stage === "download") {
      await page.getByRole("alertdialog").getByRole("button", { name: "Download and install", exact: true }).click();
    }
    const failure = requirements.getByRole("alert");
    await expect(failure).toContainText(stage === "preview" ? "Couldn't reach the download server" : "The download was interrupted");
    await expect(failure).toContainText("Check the internet connection, then try again.");
    await expect(failure).not.toContainText(/private\.example|connection reset by peer|Couldn't finish the fix/);
    expect(await calls(page, "InstallDocker")).toBe(stage === "preview" ? 0 : 1);
    await requirements.getByRole("button", { name: "Install Rancher Desktop", exact: true }).click();
    await page.getByRole("alertdialog").getByRole("button", { name: "Download and install", exact: true }).click();
    await expect(requirements).toContainText("Rancher Desktop available");
    await expect(failure).toHaveCount(0);
    expect(await calls(page, "InstallDocker")).toBe(stage === "preview" ? 1 : 2);
    expect(await page.evaluate(() => ({
      backups: window.careTest.fixtures.backups,
      recovery: window.careTest.fixtures.recovery,
      backupDir: window.careTest.fixtures.backupDir,
    }))).toEqual(saved);
    expect(await calls(page, "CleanupFailedInstall")).toBe(0);
    expect(await page.evaluate(() => window.careTest.state.setup_done)).toBe(true);
  });
}

test("unknown Windows requirements remain visible and actionable", async ({ page }) => {
  await page.clock.install();
  await openPanel(page, "panel-current", "&platform=windows");
  await page.evaluate(() => window.careTest.failNext("NetworkStatus", "PowerShell access denied"));
  await page.clock.fastForward(60_100);
  const requirements = overview(page).getByRole("region", { name: "What the clinic needs" });
  await expect(requirements).toContainText("Network profile couldn't be checked");
  await expect(requirements).not.toContainText("PowerShell");
  await requirements.getByRole("button", { name: "Check now", exact: true }).click();
  await expect(requirements).toHaveCount(0);
});

test("clipboard rejection never shows the copied toast", async ({ page }) => {
  await openPanel(page);
  await page.evaluate(() => Object.defineProperty(navigator.clipboard, "writeText", {
    configurable: true, value: async () => { throw new Error("permission denied: internal clipboard details"); },
  }));
  await overview(page).getByRole("button", { name: "Copy", exact: true }).click();
  await expect(overview(page).getByRole("alert")).toContainText("Couldn't copy the address");
  await expect(page.getByText("Address copied", { exact: true })).toHaveCount(0);
  await expect(overview(page)).not.toContainText("internal clipboard details");
});

test("QR copy uses the working mobile setup URL and Docs stays in the rail", async ({ page }) => {
  await openPanel(page);
  await page.evaluate(() => Object.defineProperty(navigator.clipboard, "writeText", {
    configurable: true, value: async (text: string) => { window.careTest.logs.push(`clipboard:${text}`); },
  }));
  await overview(page).getByRole("button", { name: "Connect a phone or tablet", exact: true }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Copy address", exact: true }).click();
  expect(await page.evaluate(() => window.careTest.logs.includes("clipboard:http://care.local/setup"))).toBe(true);
  await page.getByRole("alertdialog").getByRole("button", { name: "Done", exact: true }).click();
  await page.getByRole("button", { name: "Docs", exact: true }).click();
  expect(await page.evaluate(() => {
    const opened = window.careTest.calls.filter((call) => call.method === "OpenURL");
    return opened[opened.length - 1]?.args;
  }))
    .toEqual(["https://docs.ohc.network/"]);
});

test("CARE checking waits for events, handles failure truthfully, and can retry", async ({ page }) => {
  await openPanel(page);
  await section(page, "Updates").click();
  await expect(software(page)).toContainText("Not checked yet");
  await page.evaluate(() => {
    window.careTest.respond("CheckCareUpdate", undefined);
    window.careTest.hold("CheckCareUpdate");
  });
  await software(page).getByRole("button", { name: "Check CARE software updates" }).dblclick();
  await expect.poll(() => calls(page, "CheckCareUpdate")).toBe(1);
  await page.evaluate(() => window.careTest.release("CheckCareUpdate"));
  await expect(software(page).getByRole("button", { name: "Check CARE software updates" })).toBeDisabled();
  await page.evaluate(() => window.careTest.emit("care-check", { running: false, found: false, error: "private update fetch error" }));
  await expect(software(page)).toContainText("Couldn't check for updates");
  await expect(software(page)).not.toContainText("Up to date");
  await expect(software(page)).not.toContainText("private update fetch error");
  await software(page).getByRole("button", { name: "Try again", exact: true }).click();
  await page.evaluate(() => window.careTest.emit("care-check", { running: false, found: false }));
  await expect(software(page)).toContainText("Up to date");
});

test("a CARE update remains available after failed deferral or failed apply", async ({ page }) => {
  await openPanel(page, "panel-update");
  await section(page, "Updates").click();
  await expect(software(page)).toContainText("Ready to install");
  await page.evaluate(() => window.careTest.failNext("DismissCareUpdate", "private lock: denied"));
  await software(page).getByRole("button", { name: "Later", exact: true }).click();
  await expect(software(page)).toContainText("Ready to install");
  await expect(software(page)).not.toContainText("Saved for the next clinic start");
  await page.evaluate(() => window.careTest.failNext("ClinicAction", "private update failure"));
  await software(page).getByRole("button", { name: "Install now", exact: true }).click();
  await expect(software(page).getByRole("button", { name: "Install now", exact: true })).toBeEnabled();
  await expect(software(page)).toContainText("Ready to install");
});

test("CARE Desktop shows only native byte progress, keeps clinic status, and recovers from failure", async ({ page }) => {
  await openPanel(page, "available", "&screen=panel");
  await section(page, "Updates").click();
  await desktop(page).getByRole("button", { name: "Update CARE Desktop", exact: true }).dblclick();
  await expect.poll(() => calls(page, "InstallAppUpdate")).toBe(1);
  await expect(desktop(page)).toContainText("31.0 MB of 50.0 MB");
  await capture(page, "panel-actual-desktop-downloading-1100");
  await expect(page.getByRole("status", { name: "Clinic status: Running", exact: true })).toBeVisible();
  await page.evaluate(() => window.careTest.progress({ phase: "downloading", done: 1_000_000, total: 0 }));
  await expect(desktop(page)).toContainText("1.0 MB downloaded");
  await expect(desktop(page).getByRole("progressbar")).not.toHaveAttribute("aria-valuenow");
  await page.evaluate(() => window.careTest.progress({ phase: "verifying", done: 50_000_000, total: 50_000_000 }));
  await expect(desktop(page)).toContainText("Checking the downloaded update");
  await expect(desktop(page)).not.toContainText("100%");
  await page.evaluate(() => window.careTest.finishUpdate("private file verification: exit status 1"));
  await expect(desktop(page)).toContainText("CARE Desktop couldn't finish updating");
  await expect(desktop(page)).not.toContainText("private file verification");
  await expect(desktop(page).getByRole("button", { name: "Try again", exact: true })).toBeEnabled();
  await capture(page, "panel-actual-desktop-error-1100");
});

test("plugin changes remain locked during an external installer handoff", async ({ page }) => {
  await page.setViewportSize({ width: 720, height: 560 });
  await openPanel(page, "available", "&screen=panel");
  await section(page, "Updates").click();
  await desktop(page).getByRole("button", { name: "Update CARE Desktop", exact: true }).click();
  await expect(desktop(page)).toContainText("31.0 MB of 50.0 MB");
  await page.evaluate(() => window.careTest.progress({ phase: "installer", done: 50_000_000, total: 50_000_000 }));
  await expect(desktop(page).getByRole("button", { name: "Done", exact: true })).toBeDisabled();
  await page.evaluate(() => window.careTest.finishUpdate());
  await expect(desktop(page)).toContainText("Follow the installer to finish, then reopen CARE Desktop.");
  await expect(desktop(page).getByRole("button", { name: "Done", exact: true })).toBeEnabled();
  await capture(page, "panel-actual-installer-handoff-720");
  await section(page, "Plugins").click();
  const plugins = page.locator('[data-panel-tab="plugins"]');
  await expect(plugins.locator('[aria-label="Add a plugin"]')).toBeDisabled();
  await expect(plugins.getByRole("button", { name: "Save and apply", exact: true })).toBeDisabled();
  await capture(page, "panel-actual-plugins-installer-lock-720");
  expect(await calls(page, "SavePlugins")).toBe(0);
  await section(page, "Updates").click();
  await desktop(page).getByRole("button", { name: "Done", exact: true }).click();
  await section(page, "Plugins").click();
  await expect(plugins.locator('[aria-label="Add a plugin"]')).toBeEnabled();
});

test("active desktop updates block Plugins and Advanced independently of job acceptance", async ({ page }) => {
  await openPanel(page);
  await section(page, "Advanced").click();
  const password = page.getByLabel("Desktop admin password", { exact: true });
  const unlock = page.getByRole("button", { name: "Unlock", exact: true });
  await expect(password).toBeEnabled();
  await password.fill("preview-only-password");
  await page.evaluate(() => window.careTest.progress({ phase: "verifying", done: 50_000_000, total: 50_000_000 }));
  await expect(password).toBeDisabled();
  await expect(unlock).toBeDisabled();
  await section(page, "Plugins").click();
  await expect(page.getByRole("combobox", { name: "Add a plugin", exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Save and apply", exact: true })).toBeDisabled();
  await page.evaluate(() => window.careTest.finishUpdate("preview verification failure"));
  await section(page, "Advanced").click();
  await expect(password).toBeEnabled();
  await expect(password).toHaveValue("");
});

test("a failed startup-preference read has a safe read-only retry", async ({ page }) => {
  await page.addInitScript(() => {
    let preview: Window["careTest"];
    Object.defineProperty(window, "careTest", {
      configurable: true,
      get: () => preview,
      set: (value: Window["careTest"]) => {
        preview = value;
        value.failNext("AutostartEnabled", "read /private/startup: access denied");
      },
    });
  });
  await openPanel(page);
  const startAtLogin = clinic(page).getByRole("switch", { name: "Start when this computer starts" });
  await expect(startAtLogin).toBeDisabled();
  await expect(clinic(page)).not.toContainText("/private/startup");
  await clinic(page).getByRole("button", { name: "Check start preference" }).click();
  await expect(startAtLogin).toBeEnabled();
  await expect(startAtLogin).toBeChecked();
  expect(await calls(page, "SetAutostart")).toBe(0);
  await page.evaluate(() => window.careTest.failNext("SetAutostart", "private startup permission failure"));
  await startAtLogin.click();
  await expect(clinic(page)).toContainText("The startup setting couldn't be saved");
  await expect(startAtLogin).toBeChecked();
  await expect(startAtLogin).toBeEnabled();
  await clinic(page).getByRole("button", { name: "Check start preference" }).click();
  await expect(startAtLogin).toBeEnabled();
  await expect(startAtLogin).toBeChecked();
  await startAtLogin.click();
  await expect(startAtLogin).toBeEnabled();
  await expect(startAtLogin).not.toBeChecked();
  expect(await calls(page, "SetAutostart")).toBe(2);
});

test("reopening stopped Rancher waits for native readiness, then unlocks clinic startup", async ({ page }) => {
  await page.clock.install();
  await openPanel(page);
  await page.evaluate(() => {
    window.careTest.fixtures.health.active = false;
    window.careTest.fixtures.clinicStatus = "";
    window.careTest.fixtures.docker = { ok: false, message: "Rancher Desktop is installed but not running." };
    window.careTest.respond("DockerPlan", {
      action: "open", label: "Open Rancher Desktop", detail: "", url: "", download_preview: false,
    });
    window.careTest.hold("OpenDocker");
  });
  await page.clock.fastForward(95_000);
  await page.clock.runFor(5_100);
  await page.locator(".panel-banner").getByRole("button", { name: "See what's wrong", exact: true }).click();
  const dialog = page.getByRole("alertdialog");
  await dialog.getByRole("button", { name: "Start Rancher Desktop", exact: true }).dblclick();
  await expect.poll(() => calls(page, "OpenDocker")).toBe(1);
  const progress = dialog.getByRole("status").filter({ hasText: "Starting Rancher Desktop" });
  await expect(progress).toContainText("this can take a minute");
  await expect(dialog.getByRole("button", { name: "Close", exact: true })).toBeDisabled();
  await page.clock.fastForward(60_000);
  await expect(progress).toBeVisible();
  expect(await calls(page, "OpenDocker")).toBe(1);
  await page.evaluate(() => window.careTest.release("OpenDocker"));
  await expect(dialog.getByText("Runs the clinic software on this computer.", { exact: true })).toBeVisible();
  await expect(progress).toHaveCount(0);
  await expect(dialog.getByRole("button", { name: "Start clinic", exact: true })).toBeEnabled();
  await expect(dialog.getByRole("button", { name: "Close", exact: true })).toBeEnabled();
  expect(await calls(page, "ClinicAction")).toBe(0);
});

test("troubleshooting remains readable and keyboard accessible at minimum size", async ({ page }) => {
  await page.setViewportSize({ width: 720, height: 560 });
  await page.clock.install();
  await openPanel(page);
  await page.evaluate(() => {
    window.careTest.fixtures.health.active = false;
    window.careTest.fixtures.clinicStatus = "";
    window.careTest.fixtures.docker = { ok: false, message: "private engine failure" };
    window.careTest.respond("DockerPlan", {
      action: "open", label: "Open Rancher Desktop", detail: "", url: "", download_preview: false,
    });
  });
  await page.clock.fastForward(95_000);
  await page.clock.runFor(5_100);
  await expect(clinic(page).getByRole("heading", { name: "Not responding", exact: true })).toBeVisible();
  const open = page.locator(".panel-banner").getByRole("button", { name: "See what's wrong", exact: true });
  await open.click();
  const dialog = page.getByRole("alertdialog");
  await expect(dialog.getByRole("heading", { name: "Let's get the clinic back", exact: true })).toBeVisible();
  await expect(dialog).not.toContainText("private engine failure");
  expect(await dialog.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
  await capture(page, "panel-actual-troubleshooting-720");
  await page.evaluate(() => window.careTest.failNext("OpenDocker", "private startup details"));
  await dialog.getByRole("button", { name: "Start Rancher Desktop", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("Couldn't finish the fix");
  await expect(dialog).not.toContainText("private startup details");
  await capture(page, "panel-actual-troubleshooting-error-720");
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(open).toBeFocused();
  await fits(page);
});

test("long clinic addresses do not overflow and rail focus stays visible", async ({ page }) => {
  await page.setViewportSize({ width: 720, height: 560 });
  await page.addInitScript(() => {
    let preview: Window["careTest"];
    Object.defineProperty(window, "careTest", {
      configurable: true, get: () => preview,
      set: (value: Window["careTest"]) => {
        preview = value;
        value.state.mdns_name = `care-${"x".repeat(58)}.local`;
      },
    });
  });
  await openPanel(page);
  await page.keyboard.press("Tab");
  await expect(section(page, "Overview")).toBeFocused();
  expect(await section(page, "Overview").evaluate((element) => getComputedStyle(element).outlineWidth)).toBe("3px");
  await fits(page);
  await overview(page).getByRole("button", { name: "Connect a phone or tablet", exact: true }).click();
  const dialog = page.getByRole("alertdialog");
  expect(await dialog.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
  await page.keyboard.press("Escape");
  await section(page, "Updates").click();
  await fits(page);
});

test("Plugins and Advanced keep mounted wrappers with their separate layout contracts", async ({ page }) => {
  await openPanel(page);
  await section(page, "Plugins").click();
  const plugins = await page.locator('[data-panel-tab="plugins"]').elementHandle();
  expect(plugins).not.toBeNull();
  await expect(page.locator(".care-panel-content")).toHaveCount(0);
  await section(page, "Advanced").click();
  const advanced = await page.locator('[data-panel-tab="advanced"]').elementHandle();
  expect(advanced).not.toBeNull();
  await expect(page.locator(".care-panel-content")).toHaveCount(1);
  await section(page, "Overview").click();
  expect(await plugins!.evaluate((element) => element.isConnected)).toBe(true);
  expect(await advanced!.evaluate((element) => element.isConnected)).toBe(true);
});

test("starting CARE from troubleshooting reports failures inside the dialog and waits for native completion", async ({ page }) => {
  await page.setViewportSize({ width: 720, height: 560 });
  await page.clock.install();
  await openPanel(page);
  await page.evaluate(() => {
    window.careTest.fixtures.health.active = false;
    window.careTest.fixtures.clinicStatus = "";
  });
  await page.clock.fastForward(95_000);
  await page.clock.runFor(5_100);
  await page.locator(".panel-banner").getByRole("button", { name: "See what's wrong", exact: true }).click();
  const dialog = page.getByRole("alertdialog");
  await page.evaluate(() => window.careTest.failNext("ClinicAction", "private startup transport failure"));
  await dialog.getByRole("button", { name: "Start clinic", exact: true }).click();
  const failure = dialog.getByRole("alert");
  await expect(failure).toContainText("Couldn't finish the fix");
  await expect(failure).toBeInViewport();
  await expect(dialog).not.toContainText("private startup transport failure");
  await page.evaluate(() => {
    window.careTest.fixtures.finishJobs = false;
    window.careTest.hold("ClinicAction");
  });
  await dialog.getByRole("button", { name: "Start clinic", exact: true }).dblclick();
  await expect.poll(() => calls(page, "ClinicAction")).toBe(2);
  await expect(dialog.getByRole("button", { name: "Close", exact: true })).toBeDisabled();
  await page.evaluate(() => window.careTest.release("ClinicAction"));
  await expect(dialog.getByRole("button", { name: "Close", exact: true })).toBeDisabled();
  await expect(dialog.getByRole("status").filter({ hasText: "Working — keep CARE Desktop open." })).toBeVisible();
  await page.evaluate(() => window.careTest.finishJob("start"));
  await expect(dialog.getByRole("button", { name: "Close", exact: true })).toBeEnabled();
  await expect(dialog.getByRole("alert")).toHaveCount(0);
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
});

test("the backup summary omits filesystem paths but keeps useful backup details", async ({ page }) => {
  await openPanel(page);
  const backup = overview(page).getByRole("region", { name: "Backup summary", exact: true });
  const folder = await page.evaluate(() => window.careTest.fixtures.backupDir);
  await expect(backup).not.toContainText(folder);
  await expect(backup).not.toContainText("saved to");
  await expect(backup).toContainText("encrypted");
  await expect(backup.getByRole("button", { name: "View backups", exact: true })).toBeVisible();
});

test("database startup backup failures explain that saved backups are unaffected", async ({ page }) => {
  await openPanel(page);
  await page.evaluate(() => {
    const report = window.careTest.fixtures.storage;
    report.last_run = { ...report.last_run, state: "failed", reason: "database_unavailable" };
    window.careTest.emit("care-storage", { ...report });
  });
  await expect(page.locator(".panel-banner")).toContainText("The database wasn't ready");
  const backup = overview(page).getByRole("region", { name: "Backup summary", exact: true });
  await expect(backup).toContainText("Saved backups are unaffected");
  await expect(backup).not.toContainText("Check the backup folder");
  await section(page, "Backups").click();
  const backups = page.locator(".care-backups");
  await expect(backups.getByRole("alert")).toContainText("The database wasn't ready");
  await expect(backups.getByRole("region", { name: "Saved backups", exact: true })).toBeVisible();
  await expect(backups.getByRole("button", { name: "Choose another folder", exact: true })).toHaveCount(0);
  await expect(backups.getByRole("button", { name: "Try again now", exact: true })).toBeEnabled();
  await page.evaluate(() => {
    const report = window.careTest.fixtures.storage;
    report.last_run = { ...report.last_run, state: "ok", reason: "" };
    window.careTest.emit("care-storage", { ...report });
  });
  await expect(backups.getByRole("alert")).toHaveCount(0);
  await expect(backups).toContainText("Last completed backup");
});

test("a failed storage read does not present old backup health or location as current", async ({ page }) => {
  await openPanel(page);
  await page.evaluate(() => {
    window.careTest.fixtures.storage.last_run = {
      ...window.careTest.fixtures.storage.last_run, state: "failed", at: 1, reason: "disk_full",
    };
  });
  await section(page, "Storage").click();
  const storage = page.locator('.panel-page[aria-label="Storage"]');
  await storage.getByRole("button", { name: "Check now", exact: true }).click();
  await section(page, "Overview").click();
  const backup = overview(page).getByRole("region", { name: "Backup summary", exact: true });
  await expect(backup).toContainText("The last backup didn't finish");
  await section(page, "Storage").click();
  await expect(storage.getByRole("button", { name: "Check now", exact: true })).toBeEnabled();
  await page.evaluate(() => window.careTest.failNext("RecheckStorage", "private storage detail"));
  await storage.getByRole("button", { name: "Check now", exact: true }).click();
  await expect(storage.getByRole("alert")).toContainText("last successful check");
  await section(page, "Overview").click();
  await expect(backup).not.toContainText("Last backup failed");
  await expect(backup).not.toContainText("saved to");
  await expect(backup).toContainText("Saved");
  await expect(page.locator(".panel-banner")).toContainText("from the last successful check");
});
