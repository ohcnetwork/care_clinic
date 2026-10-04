import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { expect, test, type Page } from "@playwright/test";
import type {} from "./fixtures/host";
import { continueServerChecks } from "./helpers/setup";
import { RUN_STEPS } from "../src/lib/run-steps";
import type { AppUpdate } from "../src/types";

const password = "ClinicTest123";
const clinicName = "care-hospital";
const availableUpdate: AppUpdate = {
  current: "0.1.5", version: "0.1.6", available: true,
  notes_url: "", asset: "test-only", size: 50e6,
};
const nativeMilestones = [
  "Backup encryption ready; only the public certificate is installed.",
  "Generated a random DJANGO_SECRET_KEY in backend.env",
  "Building CARE's images - the backend and app build in the background while setup continues...",
  "Starting the secure gateway so this computer can be set up now...",
  "Waiting for the backend and app images to finish building...",
  "Starting CARE...",
  "Applying database migrations...",
  "Waiting for CARE to become healthy...",
];
const forward = (page: Page) => page.getByRole("button", { name: "Continue", exact: true });
const install = (page: Page) => page.getByRole("button", { name: "Install", exact: true });
const retry = (page: Page) => page.getByRole("button", { name: "Try again", exact: true });
const failedHeading = (page: Page) => page.getByRole("heading", { name: "Something went wrong during installation", exact: true });
const calls = (page: Page, method: string) => page.evaluate((name) =>
  window.careTest.calls.filter((call) => call.method === name).length, method);
const railLabels = (page: Page) => page.locator(".on-rail-step > span:nth-child(2)").allTextContents();

test.beforeEach(async ({ page }) => {
  // Shared-checkout edits must not replace a simulated run through Vite HMR.
  await page.routeWebSocket(/ws:\/\/127\.0\.0\.1:41783\//, (socket) => socket.send('{"type":"connected"}'));
  page.on("pageerror", (error) => { throw error; });
});

async function review(page: Page, { platform = "darwin", residue = false } = {}) {
  await page.goto(`/tests/fixtures/index.html?scenario=${residue ? "setup-cleanup" : "current"}&role=server&platform=${platform}`);
  await continueServerChecks(page, { removeResidue: residue });
  await expect(page.getByRole("heading", { name: "Choosing the clinic address", exact: true })).toBeVisible();
  await page.getByLabel("Clinic address", { exact: true }).fill(clinicName);
  await forward(page).click();
  await page.getByRole("button", { name: "Change folder", exact: true }).click();
  await page.getByRole("button", { name: "Choose where to save", exact: true }).click();
  await page.getByRole("button", { name: "Select saved file", exact: true }).click();
  await forward(page).click();
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByLabel("Confirm password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Choose where to save", exact: true }).click();
  await forward(page).click();
  await expect(page.getByRole("heading", { name: "Review before installing", exact: true })).toBeVisible();
  await expect(install(page)).toBeEnabled();
}

async function begin(page: Page, options: { platform?: string; residue?: boolean } = {}) {
  await review(page, options);
  const labels = await railLabels(page);
  // RunSetup is a safe simulated acceptance; native installation never runs.
  await page.evaluate(() => window.careTest.respond("RunSetup", undefined));
  await install(page).click();
  await expect(page.getByRole("heading", { name: "Installing CARE", exact: true })).toBeVisible();
  return labels;
}

async function fail(page: Page, offerUpdate = false) {
  if (offerUpdate) await page.evaluate((update) => window.careTest.setUpdate(update), availableUpdate);
  await page.evaluate(() => {
    window.careTest.emit("care-log", 'error: build failed: exit status 1 /private/diagnostics/fixture');
    window.careTest.emit("care-done", 1, "setup");
  });
  await expect(failedHeading(page)).toBeVisible();
  await expect(retry(page)).toBeEnabled();
  if (offerUpdate) await expect(page.getByRole("button", { name: "Update now", exact: true })).toBeEnabled();
}

async function retryableFailure(page: Page, download = true, offerUpdate = false) {
  if (offerUpdate) await page.evaluate((update) => window.careTest.setUpdate(update), availableUpdate);
  await page.evaluate((interrupted) => {
    window.careTest.fixtures.setupStarted = true;
    window.careTest.emit("care-log", "error: download failed /private/diagnostics: exit status 1");
    window.careTest.emit("setup-failed", { can_retry: true, download_interrupted: interrupted });
    window.careTest.emit("care-done", 1, "setup");
  }, download);
  await expect(page.getByRole("heading", {
    name: download ? "The download was interrupted" : "Something went wrong during installation", exact: true,
  })).toBeVisible();
  await expect(retry(page)).toBeEnabled();
  if (offerUpdate) await expect(page.getByRole("button", { name: "Update now", exact: true })).toBeEnabled();
}

const savedSetup = (page: Page) => page.evaluate(() => ({
  address: window.careTest.state.mdns_name,
  backupDir: window.careTest.fixtures.backupDir,
  password: window.careTest.fixtures.adminPassword,
  recovery: window.careTest.fixtures.recovery,
  backups: window.careTest.fixtures.backups,
}));

async function fits(page: Page) {
  expect(await page.evaluate(() => {
    const footer = document.querySelector(".install-foot")!.getBoundingClientRect();
    return {
      pageX: document.documentElement.scrollWidth > innerWidth,
      pageY: document.documentElement.scrollHeight > innerHeight,
      innerX: [...document.querySelectorAll(".install-screen, .on-main, .on-body, .on-rail, .install-content")]
        .some((element) => element.scrollWidth > element.clientWidth + 1),
      footer: footer.left >= 0 && footer.right <= innerWidth + 1 && footer.bottom <= innerHeight + 1,
    };
  })).toEqual({ pageX: false, pageY: false, innerX: false, footer: true });
  await expect(page.getByRole("complementary", { name: "Setup progress" })).toHaveCSS("width", "272px");
}

async function capture(page: Page, name: string) {
  if (!process.env.CARE_SCREENSHOTS_DIR) return;
  await mkdir(process.env.CARE_SCREENSHOTS_DIR, { recursive: true });
  await page.evaluate(() => document.fonts.ready);
  await page.screenshot({ path: join(process.env.CARE_SCREENSHOTS_DIR, `${name}.png`), animations: "disabled" });
}

test("all eight milestones match actual native log lines, not error mentions", () => {
  expect(RUN_STEPS).toHaveLength(8);
  for (const [index, line] of nativeMilestones.entries()) {
    expect(RUN_STEPS.findIndex((step) => step.re.test(line))).toBe(index);
    expect(RUN_STEPS.some((step) => step.re.test(`error: ${line}`))).toBe(false);
  }
  for (const line of ["Starting one-time setup...", "error: database migrations failed", "CARE is up to date.", "backup encryption is not set up"]) {
    expect(RUN_STEPS.some((step) => step.re.test(line))).toBe(false);
  }
});

test("installation follows log milestones without percentages, controls, or update checks", async ({ page }) => {
  await begin(page);
  const updateChecks = await calls(page, "CheckAppUpdate");
  await expect(page.getByRole("heading", { name: "Preparing the installation", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Installing CARE", exact: true })).toBeFocused();
  await expect(page.getByRole("progressbar", { name: "Installation progress", exact: true })).not.toHaveAttribute("aria-valuenow");
  await expect(page.getByRole("button")).toHaveCount(1);
  for (const [index, line] of nativeMilestones.entries()) {
    await page.evaluate((message) => window.careTest.emit("care-log", message), line);
    await expect(page.getByRole("heading", { name: RUN_STEPS[index].label, exact: true })).toBeVisible();
    await expect(page.locator(".install-stage[aria-current=step]")).toContainText(RUN_STEPS[index].label);
  }
  await expect(page.locator(".install-stage-state")).toHaveCount(0);
  await expect(page.locator(".install-screen").getByText(/^(earlier|now|latest)$/i)).toHaveCount(0);
  await page.evaluate(() => {
    window.careTest.emit("care-log", "error: Applying database migrations failed: private diagnosis");
    window.careTest.emit("care-log", "Generated a random DJANGO_SECRET_KEY in backend.env");
    window.careTest.emit("care-done", 0, "prerequisite");
    window.careTest.emit("care-done", 1, "app-update");
  });
  await expect(page.getByRole("heading", { name: "Checking the clinic", exact: true })).toBeVisible();
  await expect(page.locator(".install-screen")).not.toContainText(/\d+%|private diagnosis|Technical details|Your clinic is ready/);
  await expect(page.getByRole("button", { name: /Back|Cancel|Update/ })).toHaveCount(0);
  expect(await calls(page, "CheckAppUpdate")).toBe(updateChecks);
  expect(await calls(page, "CleanupFailedInstall")).toBe(0);
  await page.getByRole("button", { name: "Open log file", exact: true }).click();
  expect(await calls(page, "OpenLogFolder")).toBe(1);
});

for (const [platform, residue, count] of [["windows", false, 10], ["darwin", false, 8], ["darwin", true, 8]] as const) {
  test(`installation and failure keep the ${count}-step rail ${residue ? "after cleanup" : "on a clean computer"} and fit both window sizes`, async ({ page }) => {
    const labels = await begin(page, { platform, residue });
    expect(labels).toHaveLength(count);
    expect(await railLabels(page)).toEqual(labels);
    await expect(page.getByText(`Step ${count} of ${count}`, { exact: true })).toBeVisible();
    await page.evaluate((line) => window.careTest.emit("care-log", line), nativeMilestones[4]);
    for (const size of [{ width: 1100, height: 700 }, { width: 720, height: 560 }]) {
      await page.setViewportSize(size);
      await fits(page);
      await capture(page, `install-${count}-steps-${size.width}x${size.height}`);
    }
    await fail(page, true);
    expect(await railLabels(page)).toEqual(labels);
    await expect(page.locator(".on-rail-step[aria-current=step]")).toContainText("Installation stopped");
    await expect(page.locator(".install-version")).toHaveText("Version 0.1.5");
    await expect(page.getByRole("complementary").getByRole("button")).toHaveCount(0);
    for (const size of [{ width: 1100, height: 700 }, { width: 720, height: 560 }]) {
      await page.setViewportSize(size);
      await fits(page);
      await expect(retry(page)).toBeInViewport();
      await capture(page, `install-failed-${count}-steps-${size.width}x${size.height}`);
    }
    await page.locator("#install-retry-consequences").scrollIntoViewIfNeeded();
    await expect(page.getByText(/Keep your old backup recovery files/)).toBeInViewport();
    await fits(page);
  });
}

test("the warning measures 15 minutes without an actual care-log line, not total elapsed time", async ({ page }) => {
  await page.clock.install();
  await begin(page);
  await page.evaluate((line) => window.careTest.emit("care-log", line), nativeMilestones[4]);
  const warning = page.getByText("This is taking longer than usual", { exact: true });
  await page.clock.fastForward(14 * 60 * 1000 + 59 * 1000);
  await expect(warning).toHaveCount(0);
  await page.clock.fastForward(1000);
  await expect(warning).toBeVisible();
  await expect(page.getByText("Last reported stage", { exact: true })).toBeVisible();
  await expect(page.locator(".install-screen")).not.toContainText(/Slow internet|still working|still progressing/);
  await capture(page, "install-no-log-warning");
  await page.evaluate(() => window.careTest.emit("care-log", "unmatched build output - still receiving host messages"));
  await page.clock.fastForward(1000);
  await expect(warning).toHaveCount(0);
  await expect(page.getByRole("heading", { name: RUN_STEPS[4].label, exact: true })).toBeVisible();
  await page.clock.fastForward(14 * 60 * 1000 + 58 * 1000);
  await expect(warning).toHaveCount(0);
  await page.evaluate(() => window.runtime.LogPrint("a UI-originated log message is not a care-log event"));
  await page.clock.fastForward(1000);
  await expect(warning).toBeVisible();
  await expect(page.getByRole("timer")).toContainText("30:");
});

for (const first of ["setup-done", "care-done"] as const) {
  test(`completion goes straight to the panel with ${first} arriving first`, async ({ page }) => {
    await begin(page);
    await page.evaluate((event) => {
      window.careTest.state.setup_done = true;
      if (event === "setup-done") window.careTest.emit("setup-done", true);
      else window.careTest.emit("care-done", 0, "setup");
    }, first);
    await expect(page.getByRole("heading", { name: "Installing CARE", exact: true })).toBeVisible();
    await expect(page.getByText("Your clinic is ready", { exact: true })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Open control panel", exact: true })).toHaveCount(0);
    await page.evaluate((event) => {
      if (event === "setup-done") window.careTest.emit("care-done", 0, "setup");
      else window.careTest.emit("setup-done", true);
    }, first);
    await expect(page.locator(".care-panel")).toBeVisible();
    await expect(page.locator(".install-screen")).toHaveCount(0);
    expect(await calls(page, "CleanupFailedInstall")).toBe(0);
  });
}

for (const success of [true, false]) {
  test(`an early ${success ? "success" : "failure"} event is not overwritten by RunSetup acceptance`, async ({ page }) => {
    await review(page);
    await page.evaluate(() => {
      window.careTest.respond("RunSetup", undefined);
      window.careTest.hold("RunSetup");
    });
    await install(page).click();
    await expect.poll(() => calls(page, "RunSetup")).toBe(1);
    await page.evaluate((completed) => {
      if (completed) {
        window.careTest.state.setup_done = true;
        window.careTest.emit("setup-done", true);
      }
      window.careTest.emit("care-done", completed ? 0 : 1, "setup");
      window.careTest.release("RunSetup");
    }, success);
    if (success) await expect(page.locator(".care-panel")).toBeVisible();
    else await expect(failedHeading(page)).toBeVisible();
    await expect(page.getByRole("heading", { name: "Installing CARE", exact: true })).toHaveCount(0);
  });
}

test("failure is honest about partial setup, keeps diagnostics in the log, and performs no cleanup on entry", async ({ page }) => {
  await begin(page);
  await fail(page);
  await expect(failedHeading(page)).toBeFocused();
  await expect(page.locator(".install-screen")).toContainText("Installation stopped. Your clinic isn't ready to use yet.");
  await expect(page.locator(".install-screen")).not.toContainText(/nothing is running|nothing was saved|throw.*away|exit status|\/private\/diagnostics|last output/i);
  await expect(page.locator(".install-screen pre")).toHaveCount(0);
  await expect(retry(page)).toHaveAttribute("aria-describedby", "install-retry-consequences");
  await expect(page.locator("#install-retry-consequences")).toContainText("clinic address and existing backups are kept");
  await expect(page.locator("#install-retry-consequences")).toContainText("old CARE Clinic admin codes stop working");
  await expect(page.locator("#install-retry-consequences")).toContainText("Keep your old backup recovery files");
  expect(await calls(page, "CleanupFailedInstall")).toBe(0);
  await page.evaluate(() => window.careTest.failNext("OpenLogFolder", "open /private/diagnostics: exit status 1"));
  await page.getByRole("button", { name: "Open log file", exact: true }).click();
  await expect(page.getByText("Couldn't open the log file.", { exact: false })).toBeVisible();
  await expect(page.locator(".install-screen")).not.toContainText("/private/diagnostics");
});

for (const [error, friendly] of [
  ["permission denied at /private/install: exit status 1", "The unfinished installation couldn't be cleared."],
  ["User cancelled the authorization dialog (-128)", "Cleanup was cancelled."],
] as const) {
  test(`retry handles ${friendly.toLowerCase()} and preserves backups while resetting setup choices`, async ({ page }) => {
    await begin(page);
    await fail(page, true);
    const backups = await page.evaluate(() => window.careTest.fixtures.backups);
    await page.evaluate((detail) => {
      window.careTest.hold("CleanupFailedInstall");
      window.careTest.failNext("CleanupFailedInstall", detail);
    }, error);
    await retry(page).evaluate((button: HTMLButtonElement) => { button.click(); button.click(); });
    await expect.poll(() => calls(page, "CleanupFailedInstall")).toBe(1);
    await expect(page.getByRole("button", { name: "Back to setup", exact: true })).toBeDisabled();
    await expect(page.getByRole("button", { name: "Update now", exact: true })).toBeDisabled();
    await page.evaluate(() => window.careTest.release("CleanupFailedInstall"));
    await expect(page.getByText(friendly, { exact: false })).toBeVisible();
    await expect(failedHeading(page)).toBeVisible();
    await expect(page.locator(".install-screen")).not.toContainText(error);
    await expect(retry(page)).toBeEnabled();
    await retry(page).click();
    await continueServerChecks(page);
    await expect(page.getByRole("heading", { name: "Choosing the clinic address", exact: true })).toBeVisible();
    await expect(page.getByLabel("Clinic address", { exact: true })).toHaveValue(clinicName);
    expect(await page.evaluate(() => window.careTest.fixtures.backups)).toEqual(backups);
    expect(await page.evaluate(() => window.careTest.fixtures.recovery)).toMatchObject({ backup_saved: false, backup_verified: false, codes_saved: false });
    await forward(page).click();
    await expect(page.getByText("/test-fixtures/Desktop/care-db-backups", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Choose where to save", exact: true }).click();
    await page.getByRole("button", { name: "Select saved file", exact: true }).click();
    await forward(page).click();
    await expect(page.getByLabel("Password", { exact: true })).toHaveValue("");
    await expect(page.getByLabel("Confirm password", { exact: true })).toHaveValue("");
    expect(await calls(page, "RunSetup")).toBe(1);
  });
}

test("Back to setup returns quietly without cleanup or discarding saved choices", async ({ page }) => {
  await begin(page);
  await fail(page);
  await page.getByRole("button", { name: "Back to setup", exact: true }).click();
  await continueServerChecks(page);
  await expect(page.getByRole("heading", { name: "Choosing the clinic address", exact: true })).toBeVisible();
  expect(await calls(page, "CleanupFailedInstall")).toBe(0);
  await expect(page.getByLabel("Clinic address", { exact: true })).toHaveValue(clinicName);
  await forward(page).click();
  await expect(page.getByText("/test-fixtures/CLINIC-BACKUP/care-db-backups", { exact: true })).toBeVisible();
  await forward(page).click();
  await expect(page.getByLabel("Password", { exact: true })).toHaveValue(password);
  expect(await page.evaluate(() => window.careTest.fixtures.recovery.codes_saved)).toBe(true);
});

test("an interrupted download explains reconnecting without offering cleanup or an app update", async ({ page }) => {
  await begin(page);
  const updateChecks = await calls(page, "CheckAppUpdate");
  await retryableFailure(page);
  await expect(page.getByRole("heading", { name: "The download was interrupted", exact: true })).toBeFocused();
  await expect(page.locator(".install-screen")).toContainText("Keep CARE Clinic open, reconnect to the internet, then try again.");
  await expect(page.locator("#install-retry-consequences")).toContainText("clinic address, backup folder, admin password and saved recovery files");
  await expect(page.locator(".install-screen")).not.toContainText(/exit status|\/private\/diagnostics|old CARE Clinic admin codes stop working|clears the unfinished install first/);
  await page.mouse.move(0, 0);
  await expect(retry(page)).toHaveCSS("background-color", "rgb(5, 122, 85)");
  await expect(page.getByRole("button", { name: /Update now|Check again/ })).toHaveCount(0);
  expect(await calls(page, "CheckAppUpdate")).toBe(updateChecks);
  expect(await calls(page, "CleanupFailedInstall")).toBe(0);
  for (const size of [{ width: 1100, height: 700 }, { width: 720, height: 560 }]) {
    await page.setViewportSize(size);
    await fits(page);
    await expect(retry(page)).toBeInViewport();
  }
});

test("repeated installation retries preserve the original setup until native success", async ({ page }) => {
  const labels = await begin(page);
  await retryableFailure(page);
  const saved = await savedSetup(page);
  for (let attempt = 1; attempt <= 2; attempt++) {
    await retry(page).click();
    await expect(page.getByRole("heading", { name: "Installing CARE", exact: true })).toBeVisible();
    await expect.poll(() => calls(page, "RetrySetup")).toBe(attempt);
    expect(await railLabels(page)).toEqual(labels);
    expect(await savedSetup(page)).toEqual(saved);
    expect(await calls(page, "RunSetup")).toBe(1);
    expect(await calls(page, "CleanupFailedInstall")).toBe(0);
    await expect(page.locator(".care-panel")).toHaveCount(0);
    if (attempt === 1) await retryableFailure(page);
  }
  expect(await page.evaluate(() => window.careTest.calls.filter((call) => call.method === "RetrySetup").map((call) => call.args))).toEqual([[], []]);
  await page.evaluate(() => {
    window.careTest.state.setup_done = true;
    window.careTest.emit("setup-done", true);
  });
  await expect(page.getByRole("heading", { name: "Installing CARE", exact: true })).toBeVisible();
  await page.evaluate(() => window.careTest.emit("care-done", 0, "setup"));
  await expect(page.locator(".care-panel")).toBeVisible();
  expect(await calls(page, "CleanupFailedInstall")).toBe(0);
});

test("a rejected installation retry stays recoverable and cannot run twice", async ({ page }) => {
  await begin(page);
  await retryableFailure(page);
  const saved = await savedSetup(page);
  await page.evaluate(() => {
    window.careTest.hold("RetrySetup");
    window.careTest.failNext("RetrySetup", "missing backup location /private/diagnostics");
  });
  await retry(page).evaluate((button: HTMLButtonElement) => { button.click(); button.click(); });
  await expect.poll(() => calls(page, "RetrySetup")).toBe(1);
  await expect(page.getByRole("button", { name: "Back to setup", exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Retrying installation…", exact: true })).toBeDisabled();
  await expect(page.locator(".install-screen")).not.toContainText("Clearing the unfinished installation");
  await page.evaluate(() => window.careTest.release("RetrySetup"));
  await expect(page.getByText("Your saved setup has been kept.", { exact: false })).toBeVisible();
  await expect(page.locator(".install-screen")).not.toContainText("/private/diagnostics");
  await expect(retry(page)).toBeEnabled();
  expect(await savedSetup(page)).toEqual(saved);
  await retry(page).click();
  await expect(page.getByRole("heading", { name: "Installing CARE", exact: true })).toBeVisible();
  expect(await calls(page, "RetrySetup")).toBe(2);
  expect(await calls(page, "CleanupFailedInstall")).toBe(0);
});

for (const first of ["setup-done", "care-done"] as const) {
  test(`an early retry success with ${first} first is not overwritten by acceptance`, async ({ page }) => {
    await begin(page);
    await retryableFailure(page);
    await page.evaluate(() => {
      window.careTest.respond("RetrySetup", undefined);
      window.careTest.hold("RetrySetup");
    });
    await retry(page).click();
    await expect.poll(() => calls(page, "RetrySetup")).toBe(1);
    await page.evaluate((event) => {
      window.careTest.state.setup_done = true;
      if (event === "setup-done") window.careTest.emit("setup-done", true);
      else window.careTest.emit("care-done", 0, "setup");
      window.careTest.release("RetrySetup");
    }, first);
    await expect(page.getByRole("heading", { name: "Installing CARE", exact: true })).toBeVisible();
    await expect(page.locator(".care-panel")).toHaveCount(0);
    await page.evaluate((event) => {
      if (event === "setup-done") window.careTest.emit("care-done", 0, "setup");
      else window.careTest.emit("setup-done", true);
    }, first);
    await expect(page.locator(".care-panel")).toBeVisible();
    expect(await calls(page, "CleanupFailedInstall")).toBe(0);
  });
}

test("retry failure metadata survives acceptance before the completion event", async ({ page }) => {
  await begin(page);
  await retryableFailure(page);
  await page.evaluate(() => window.careTest.hold("RetrySetup"));
  await retry(page).click();
  await expect.poll(() => calls(page, "RetrySetup")).toBe(1);
  await page.evaluate(() => {
    window.careTest.emit("setup-failed", { can_retry: true, download_interrupted: false });
    window.careTest.release("RetrySetup");
  });
  await expect(page.getByRole("heading", { name: "Installing CARE", exact: true })).toBeVisible();
  await page.evaluate(() => window.careTest.emit("care-done", 1, "setup"));
  await expect(failedHeading(page)).toBeVisible();
  await expect(page.getByText("Your setup choices are kept", { exact: true })).toBeVisible();
  await retry(page).click();
  await expect.poll(() => calls(page, "RetrySetup")).toBe(2);
  expect(await calls(page, "CleanupFailedInstall")).toBe(0);
});

test("an early retry failure is not replaced by late acceptance or unrelated failure metadata", async ({ page }) => {
  await begin(page);
  await retryableFailure(page);
  await page.evaluate(() => {
    window.careTest.respond("RetrySetup", undefined);
    window.careTest.hold("RetrySetup");
  });
  await retry(page).click();
  await expect.poll(() => calls(page, "RetrySetup")).toBe(1);
  await page.evaluate(() => {
    window.careTest.emit("setup-failed", { can_retry: true, download_interrupted: true });
    window.careTest.emit("care-done", 1, "setup");
    window.careTest.release("RetrySetup");
    window.careTest.emit("setup-failed", { can_retry: false, download_interrupted: false });
  });
  await expect(page.getByRole("heading", { name: "The download was interrupted", exact: true })).toBeVisible();
  await expect(retry(page)).toBeEnabled();
  await retry(page).click();
  await expect.poll(() => calls(page, "RetrySetup")).toBe(2);
  expect(await calls(page, "CleanupFailedInstall")).toBe(0);
});

for (const first of ["retry", "update"] as const) {
  test(`resuming setup, updating, and Back cannot race when ${first} is clicked first`, async ({ page }) => {
    await begin(page);
    await retryableFailure(page, false, true);
    await page.evaluate((action) => {
      window.careTest.hold("RetrySetup");
      const button = (label: string) => [...document.querySelectorAll("button")].find((item) => item.textContent?.trim() === label)!;
      const retry = button("Try again");
      const update = button("Update now");
      const back = button("Back to setup");
      if (action === "retry") { retry.click(); update.click(); }
      else { update.click(); retry.click(); }
      back.click();
    }, first);
    if (first === "retry") {
      await expect.poll(() => calls(page, "RetrySetup")).toBe(1);
      expect(await calls(page, "InstallAppUpdate")).toBe(0);
      await page.evaluate(() => window.careTest.release("RetrySetup"));
      await expect(page.getByRole("heading", { name: "Installing CARE", exact: true })).toBeVisible();
    } else {
      await expect.poll(() => calls(page, "InstallAppUpdate")).toBe(1);
      expect(await calls(page, "RetrySetup")).toBe(0);
      await expect(retry(page)).toBeDisabled();
      await page.evaluate(() => {
        window.careTest.emit("setup-failed", { can_retry: false, download_interrupted: true });
        window.careTest.finishUpdate("preview update cancelled");
      });
      await expect(failedHeading(page)).toBeVisible();
      await expect(retry(page)).toBeEnabled();
    }
    expect(await calls(page, "CleanupFailedInstall")).toBe(0);
  });
}

for (const failure of ["couldn't reach GitHub to check for updates: private transport error", "release 0.1.6 has no installer for this computer"]) {
  test(`an unavailable update check never becomes an offer: ${failure.slice(0, 25)}`, async ({ page }) => {
    await begin(page);
    await page.evaluate((error) => {
      window.careTest.hold("CheckAppUpdate");
      window.careTest.failNext("CheckAppUpdate", error);
    }, failure);
    await fail(page);
    await expect(page.getByText("Checking for a CARE Clinic update…", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "Update now", exact: true })).toHaveCount(0);
    await expect(retry(page)).toBeEnabled();
    await page.evaluate(() => window.careTest.release("CheckAppUpdate"));
    await expect(page.getByRole("button", { name: "Check again", exact: true })).toBeEnabled();
    await expect(page.getByRole("button", { name: "Update now", exact: true })).toHaveCount(0);
    await expect(page.locator(".install-screen")).not.toContainText(/Up to date|private transport error|seems to be offline/);
    await page.evaluate((update) => window.careTest.setUpdate(update), availableUpdate);
    await page.getByRole("button", { name: "Check again", exact: true }).click();
    await expect(page.getByRole("button", { name: "Update now", exact: true })).toBeEnabled();
    expect(await calls(page, "InstallAppUpdate")).toBe(0);
  });
}

test("an update stays busy after job acceptance and failure offers a separate update retry", async ({ page }) => {
  await begin(page);
  await fail(page, true);
  await page.getByRole("button", { name: "Update now", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Downloading CARE Clinic 0.1.6…", exact: true })).toBeVisible();
  await expect(retry(page)).toBeDisabled();
  await expect(page.getByRole("button", { name: "Back to setup", exact: true })).toBeDisabled();
  await page.evaluate(() => window.careTest.finishUpdate("couldn't replace the application: /private/failure exit status 1"));
  await expect(page.getByRole("heading", { name: "The CARE Clinic update didn't finish", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Retry update", exact: true })).toBeEnabled();
  await expect(retry(page)).toBeEnabled();
  await expect(page.locator(".install-screen")).not.toContainText("/private/failure");
  await page.getByRole("button", { name: "Retry update", exact: true }).click();
  await expect.poll(() => calls(page, "InstallAppUpdate")).toBe(2);
  expect(await calls(page, "CleanupFailedInstall")).toBe(0);
  for (const [phase, heading] of [
    ["verifying", "Checking the CARE Clinic download…"],
    ["installing", "Updating the CARE Clinic application…"],
    ["installer", "The CARE Clinic installer has opened"],
  ] as const) {
    await page.evaluate((next) => window.careTest.progress({ phase: next, done: 50e6, total: 50e6 }), phase);
    await expect(page.getByRole("heading", { name: heading, exact: true })).toBeVisible();
    await expect(retry(page)).toBeDisabled();
  }
  await page.evaluate(() => window.careTest.finishUpdate());
  await expect(page.getByText("Finish updating in the installer, then reopen CARE Clinic before trying setup again.", { exact: true })).toBeVisible();
  await expect(retry(page)).toBeDisabled();
});

for (const first of ["retry", "update"] as const) {
  test(`cleanup, update, and Back cannot race when ${first} is clicked first`, async ({ page }) => {
    await begin(page);
    await fail(page, true);
    await page.evaluate((action) => {
      window.careTest.hold("CleanupFailedInstall");
      const button = (label: string) => [...document.querySelectorAll("button")].find((item) => item.textContent?.trim() === label)!;
      const retry = button("Try again");
      const update = button("Update now");
      const back = button("Back to setup");
      if (action === "retry") { retry.click(); update.click(); }
      else { update.click(); retry.click(); }
      back.click();
    }, first);
    if (first === "retry") {
      await expect.poll(() => calls(page, "CleanupFailedInstall")).toBe(1);
      expect(await calls(page, "InstallAppUpdate")).toBe(0);
      await expect(failedHeading(page)).toBeVisible();
      await page.evaluate(() => window.careTest.release("CleanupFailedInstall"));
      await continueServerChecks(page);
      await expect(page.getByRole("heading", { name: "Choosing the clinic address", exact: true })).toBeVisible();
    } else {
      await expect.poll(() => calls(page, "InstallAppUpdate")).toBe(1);
      expect(await calls(page, "CleanupFailedInstall")).toBe(0);
      await expect(failedHeading(page)).toBeVisible();
      await expect(retry(page)).toBeDisabled();
      await page.evaluate(() => window.careTest.finishUpdate("preview update cancelled"));
      await expect(retry(page)).toBeEnabled();
    }
  });
}

test("reduced motion keeps the current stage readable without animation", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.setViewportSize({ width: 720, height: 560 });
  await begin(page);
  await page.evaluate((line) => window.careTest.emit("care-log", line), nativeMilestones[4]);
  await expect(page.locator(".install-progress [data-slot=progress-indicator]")).toHaveCSS("animation-name", "none");
  await expect(page.getByRole("heading", { name: RUN_STEPS[4].label, exact: true })).toBeVisible();
  await page.keyboard.press("Tab");
  await expect(page.getByRole("button", { name: "Open log file", exact: true })).toBeFocused();
  await expect(page.getByRole("button", { name: "Open log file", exact: true })).toBeInViewport();
  await fits(page);
});

test("small installation text and destructive actions retain readable contrast", async ({ page }) => {
  await begin(page);
  const contrast = async (selectors: string[]) => page.evaluate((targets) => {
    const values = (color: string) => (color.match(/[\d.]+/g) || []).map(Number);
    const luminance = (color: number[]) => color.slice(0, 3).map((value) => {
      const channel = value / 255;
      return channel <= .04045 ? channel / 12.92 : ((channel + .055) / 1.055) ** 2.4;
    }).reduce((sum, channel, index) => sum + channel * [.2126, .7152, .0722][index], 0);
    return targets.map((selector) => {
      const element = document.querySelector(selector)!;
      const text = luminance(values(getComputedStyle(element).color));
      let background = values("rgb(255,255,255)");
      for (let parent: Element | null = element; parent; parent = parent.parentElement) {
        const color = values(getComputedStyle(parent).backgroundColor);
        if (color.length === 3 || color[3] === 1) { background = color; break; }
      }
      const surface = luminance(background);
      return { selector, ratio: (Math.max(text, surface) + .05) / (Math.min(text, surface) + .05) };
    });
  }, selectors);
  for (const result of await contrast([".on-subtitle", ".install-elapsed", ".install-support > p", ".install-support .on-log button", ".on-foot-note"])) {
    expect(result.ratio, result.selector).toBeGreaterThanOrEqual(4.5);
  }
  await fail(page, true);
  for (const result of await contrast([".install-update-primary", ".install-retry-action", ".install-failure-card p", ".install-update p"])) {
    expect(result.ratio, result.selector).toBeGreaterThanOrEqual(4.5);
  }
});
