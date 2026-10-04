import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { expect, test, type Page } from "@playwright/test";

import type { AppUpdateProgress } from "../src/types";
import type {} from "./fixtures/host";
import { continueServerChecks } from "./helpers/setup";

const connectName = "Connect to an existing server on the local network";
const setup = (page: Page) => page.getByRole("button", { name: "Start setup", exact: true });
const connect = (page: Page) => page.getByRole("button", { name: connectName, exact: true });
const methodCount = (page: Page, method: string) =>
  page.evaluate((name) => window.careTest.calls.filter((call) => call.method === name).length, method);

let browserErrors: string[] = [];
test.beforeEach(async ({ page, baseURL }) => {
  // Keep an in-flight flow stable when another agent edits this shared checkout.
  const socketOrigin = new URL(baseURL ?? "http://127.0.0.1:41783").origin.replace(/^http/, "ws");
  await page.routeWebSocket(`${socketOrigin}/**`, (socket) => socket.send('{"type":"connected"}'));
  browserErrors = [];
  page.on("pageerror", (error) => browserErrors.push(error.message));
});
test.afterEach(() => {
  expect(browserErrors).toEqual([]);
});

async function visit(page: Page, scenario = "current") {
  await page.goto(`/tests/fixtures/index.html?scenario=${scenario}`);
  await expect(page.getByRole("heading", { name: "Set up CARE on this computer" })).toBeVisible();
  await expect(page.getByText("Checking for updates\u2026", { exact: true })).toHaveCount(0);
}

async function beginUpdate(page: Page) {
  await visit(page, "available");
  await page.getByRole("button", { name: "Update now", exact: true }).click();
  await expect(page.getByText("Downloading CARE Clinic 0.1.6\u2026")).toBeVisible();
}

async function screenshot(page: Page, name: string) {
  const directory = process.env.CARE_SCREENSHOTS_DIR;
  if (!directory) return;
  await mkdir(directory, { recursive: true });
  await page.evaluate(() => document.fonts.ready);
  await page.screenshot({ path: join(directory, `${name}.png`), animations: "disabled" });
}

test("start and client navigation do not persist a role or scan for residue", async ({ page }) => {
  await visit(page);
  await expect(page.getByText("Your clinic's records, running on this computer.")).toBeVisible();
  await expect(page.getByText("Takes about 20 minutes. Internet is needed.")).toBeVisible();
  await expect(page.getByText("Up to date", { exact: true })).toBeVisible();
  await expect.poll(() => methodCount(page, "CheckAppUpdate")).toBe(1);
  expect(await page.evaluate(() => window.careTest.state.role)).toBe("");
  for (const method of ["SelectRole", "BeginServerSetup", "ScanResidue"]) {
    expect(await methodCount(page, method)).toBe(0);
  }

  await connect(page).click();
  await expect(page.getByRole("heading", { name: "Find your clinic's server" })).toBeVisible();
  expect(await page.evaluate(() => window.careTest.state.role)).toBe("");
  expect(await methodCount(page, "ConnectClient")).toBe(0);
  expect(await methodCount(page, "SelectRole")).toBe(0);
  await page.getByRole("button", { name: "Back", exact: true }).click();
  await expect(setup(page)).toBeVisible();
  expect(await page.evaluate(() => window.careTest.state.role)).toBe("");
});

test("setup initializes once on its own screen before any guarded reads or writes", async ({ page }) => {
  await visit(page);
  await page.evaluate(() => window.careTest.hold("BeginServerSetup"));
  await setup(page).evaluate((button: HTMLButtonElement) => { button.click(); button.click(); });
  await expect(page.getByRole("heading", { name: "Room for the clinic", exact: true })).toBeVisible();
  await expect(page.getByText("Preparing setup\u2026", { exact: true }).first()).toBeVisible();
  expect(await methodCount(page, "BeginServerSetup")).toBe(1);
  for (const method of ["SetMDNSName", "GetSetupRecoveryStatus", "BackupDirSpace"]) {
    expect(await methodCount(page, method)).toBe(0);
  }
  await expect(page.getByRole("button", { name: "Back", exact: true })).toBeDisabled();
  await page.evaluate(() => window.careTest.release("BeginServerSetup"));
  await expect(page.getByRole("button", { name: "Continue", exact: true })).toBeEnabled();
  expect(await methodCount(page, "SetMDNSName")).toBe(0);
  await continueServerChecks(page);
  await expect(page.getByLabel("Clinic address", { exact: true })).toBeVisible();
  await expect.poll(() => methodCount(page, "SetMDNSName")).toBeGreaterThan(0);
  expect(await page.evaluate(() => window.careTest.state.role)).toBe("server");
  expect(await methodCount(page, "SelectRole")).toBe(0);

  await page.getByRole("button", { name: "Back", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Removing stale files from an earlier setup", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Back", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Installing what CARE needs" })).toBeVisible();
  await page.getByRole("button", { name: "Back", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Room for the clinic", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Back", exact: true }).click();
  await expect(setup(page)).toBeVisible();
  expect(await page.evaluate(() => window.careTest.state.role)).toBe("");
  await connect(page).click();
  await expect(page.getByRole("heading", { name: "Find your clinic's server" })).toBeVisible();
  expect(await page.evaluate(() => window.careTest.state.role)).toBe("");
});

test("setup failure stays in setup, is readable, and can be retried without bypassing the guard", async ({ page }) => {
  await visit(page);
  await page.evaluate(() => window.careTest.failNext("BeginServerSetup", "could not save settings: /private/config.json: EACCES"));
  await setup(page).click();
  await expect(page.getByRole("alert")).toContainText("Couldn't start setup");
  await expect(page.getByText(/EACCES|private\/config/)).toHaveCount(0);
  expect(await methodCount(page, "SetMDNSName")).toBe(0);
  expect(await methodCount(page, "GetSetupRecoveryStatus")).toBe(0);
  await page.getByRole("button", { name: "Try again", exact: true }).click();
  await continueServerChecks(page);
  await expect(page.getByLabel("Clinic address", { exact: true })).toBeVisible();
  expect(await methodCount(page, "BeginServerSetup")).toBe(2);
});

test("an existing role lock is not bypassed by start navigation", async ({ page }) => {
  await visit(page);
  await page.evaluate(() => { window.careTest.state.role = "client"; });
  await setup(page).click();
  await expect(page.getByRole("alert")).toContainText("Couldn't start setup");
  expect(await methodCount(page, "SetMDNSName")).toBe(0);
  expect(await page.evaluate(() => window.careTest.state.role)).toBe("client");
});

test("saved roles still bypass the start page", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?role=client");
  await expect(page.getByRole("heading", { name: "Find your clinic's server" })).toBeVisible();
  await expect(setup(page)).toHaveCount(0);
  expect(await methodCount(page, "BeginServerSetup")).toBe(0);
});

test("the client update card receives progress and failure feedback", async ({ page }) => {
  await visit(page, "available");
  await connect(page).click();
  await expect(page.getByRole("heading", { name: "Find your clinic's server" })).toBeVisible();
  await page.getByRole("button", { name: "Update now", exact: true }).click();
  await expect(page.getByText("Downloading CARE Clinic 0.1.6\u2026", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Back", exact: true })).toBeDisabled();
  await page.evaluate(() => window.careTest.finishUpdate("native update failed with private details"));
  await expect(page.getByRole("alert").first()).toContainText("Your current version was kept");
  await expect(page.getByRole("button", { name: "Back", exact: true })).toBeEnabled();
  await expect(page.getByText("native update failed with private details")).toHaveCount(0);
});

test("checking is single-flight, nonblocking, and never claims success before a response", async ({ page }) => {
  await visit(page);
  await page.evaluate(() => window.careTest.hold("CheckAppUpdate"));
  await page.getByRole("button", { name: "Check for updates", exact: true })
    .evaluate((button: HTMLButtonElement) => { button.click(); button.click(); });
  await expect(page.getByText("Checking for updates\u2026", { exact: true })).toBeVisible();
  await expect(page.getByText("Up to date", { exact: true })).toHaveCount(0);
  expect(await methodCount(page, "CheckAppUpdate")).toBe(2);
  await expect(setup(page)).toBeEnabled();
  await expect(connect(page)).toBeEnabled();
  await page.evaluate(() => window.careTest.release("CheckAppUpdate"));
  await expect(page.getByText("Up to date", { exact: true })).toBeVisible();
});

test("available updates keep both paths usable and open the release notes", async ({ page }) => {
  await visit(page, "available");
  await expect(page.getByText("CARE Clinic 0.1.6", { exact: true })).toBeVisible();
  await expect(setup(page)).toBeEnabled();
  await expect(connect(page)).toBeEnabled();
  await page.getByRole("button", { name: "What's new", exact: true }).click();
  expect(await page.evaluate(() => window.careTest.calls.find((call) => call.method === "OpenURL")?.args))
    .toEqual(["https://github.com/ohcnetwork/care_desktop/releases"]);
});

test("update acceptance is not completion and duplicate clicks start only one job", async ({ page }) => {
  await visit(page, "available");
  await page.evaluate(() => window.careTest.hold("InstallAppUpdate"));
  await page.getByRole("button", { name: "Update now", exact: true })
    .evaluate((button: HTMLButtonElement) => { button.click(); button.click(); });
  await expect(page.getByText("Preparing the update\u2026")).toBeVisible();
  expect(await methodCount(page, "InstallAppUpdate")).toBe(1);
  await expect(setup(page)).toBeDisabled();
  await expect(connect(page)).toBeDisabled();
  await page.evaluate(() => window.careTest.release("InstallAppUpdate"));
  await expect(page.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "62");
  await expect(page.getByText("31.0 MB of 50.0 MB")).toBeVisible();
  await expect(setup(page)).toBeDisabled();
  await page.evaluate(() => window.careTest.emit("care-done", 0, "backup-now"));
  await expect(connect(page)).toBeDisabled();
  expect(await page.evaluate(() => window.careTest.state.role)).toBe("");
});

for (const first of ["update", "setup", "client"] as const) {
  test(`start navigation and updating are mutually exclusive when ${first} starts first`, async ({ page }) => {
    await visit(page, "available");
    await page.evaluate((action) => {
      const button = (text: string) => [...document.querySelectorAll("button")].find((item) => item.textContent?.trim() === text)!;
      const update = button("Update now");
      const setup = button("Start setup");
      const client = document.querySelector<HTMLButtonElement>(".start-connect")!;
      if (action === "update") { update.click(); setup.click(); client.click(); }
      else { (action === "setup" ? setup : client).click(); update.click(); }
    }, first);
    if (first === "update") {
      await expect(page.getByText("Downloading CARE Clinic 0.1.6\u2026")).toBeVisible();
      expect(await methodCount(page, "InstallAppUpdate")).toBe(1);
      expect(await methodCount(page, "BeginServerSetup")).toBe(0);
      expect(await page.evaluate(() => window.careTest.state.role)).toBe("");
    } else {
      await expect(page.getByRole("heading", { name: first === "setup" ? "Room for the clinic" : "Find your clinic's server" })).toBeVisible();
      expect(await methodCount(page, "InstallAppUpdate")).toBe(0);
    }
  });
}

test("unknown totals use indeterminate progress, and percentages are bounded", async ({ page }) => {
  await beginUpdate(page);
  await page.evaluate(() => window.careTest.progress({ phase: "downloading", done: 1e6, total: 0 }));
  await expect(page.getByRole("progressbar")).not.toHaveAttribute("aria-valuenow");
  await expect(page.getByText("1.0 MB downloaded")).toBeVisible();
  await page.evaluate(() => window.careTest.progress({ phase: "downloading", done: 60e6, total: 50e6 }));
  await expect(page.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "100");
});

for (const phase of ["verifying", "installing", "restarting", "installer"] as const) {
  test(`real ${phase} events render their state and retain the navigation lock`, async ({ page }) => {
    await beginUpdate(page);
    await page.evaluate((value: AppUpdateProgress["phase"]) =>
      window.careTest.progress({ phase: value, done: 0, total: 0 }), phase);
    const expected = {
      verifying: "Checking CARE Clinic 0.1.6\u2026",
      installing: "Installing CARE Clinic 0.1.6\u2026",
      restarting: "Restarting CARE Clinic to finish updating\u2026",
      installer: "The installer has opened",
    }[phase];
    await expect(page.getByText(expected, { exact: true })).toBeVisible();
    await expect(setup(page)).toBeDisabled();
    if (phase === "installer" || phase === "restarting") {
      if (phase === "installer") await expect(page.getByRole("button", { name: "OK", exact: true })).toBeDisabled();
      await page.evaluate(() => window.careTest.finishUpdate());
      await expect(setup(page)).toBeDisabled();
      await expect(connect(page)).toBeDisabled();
      if (phase === "installer") {
        await expect(page.getByRole("button", { name: "OK", exact: true })).toBeEnabled();
      } else {
        await expect(page.getByRole("button", { name: /^(OK|Done|Dismiss)$/ })).toHaveCount(0);
      }
      await expect(page.getByText("Up to date", { exact: true })).toHaveCount(0);
      await expect(page.getByRole("button", { name: "Check for updates", exact: true })).toHaveCount(0);
    } else {
      await expect(page.getByRole("progressbar")).not.toHaveAttribute("aria-valuenow");
    }
    if (phase === "installer") {
      await expect(page.getByText("Follow it to finish updating, then reopen CARE Clinic.")).toBeVisible();
      await page.getByRole("button", { name: "OK", exact: true }).click();
      await expect(setup(page)).toBeEnabled();
      await expect(connect(page)).toBeEnabled();
      await expect(page.getByText("Up to date", { exact: true })).toHaveCount(0);
    }
  });
}

test("failed and cancelled updates keep technical details out of the UI and allow retry", async ({ page }) => {
  await beginUpdate(page);
  await page.evaluate(() => window.careTest.finishUpdate(
    "couldn't replace CARE Clinic with version 0.1.6, so the current version was kept: User canceled (-128)",
  ));
  await expect(page.getByRole("alert")).toContainText("Your current version was kept");
  await expect(page.getByText(/-128|User canceled|couldn't replace/)).toHaveCount(0);
  await expect(setup(page)).toBeEnabled();
  await expect(connect(page)).toBeEnabled();
  await page.getByRole("button", { name: "Open log file", exact: true }).click();
  expect(await methodCount(page, "OpenLogFolder")).toBe(1);
  await page.getByRole("button", { name: "Try again", exact: true }).click();
  await expect(page.getByText("Downloading CARE Clinic 0.1.6\u2026")).toBeVisible();
  expect(await methodCount(page, "InstallAppUpdate")).toBe(2);
});

test("a damaged download has its specific retry state, without exposing checksums", async ({ page }) => {
  await beginUpdate(page);
  await page.evaluate(() => window.careTest.finishUpdate(
    "the CARE Clinic 0.1.6 update didn't download properly and was deleted without being installed (its SHA-256 is secret)",
  ));
  await expect(page.getByRole("alert")).toContainText("The download didn't come through properly");
  await expect(page.getByText("Nothing was installed. Try again on a steadier connection.")).toBeVisible();
  await expect(page.getByText(/SHA-256|secret/)).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Open log file", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Try again", exact: true }).click();
  await expect(page.getByRole("progressbar")).toBeVisible();
});

test("synchronous update rejection clears the busy state and displays a friendly failure", async ({ page }) => {
  await visit(page, "available");
  await page.evaluate(() => window.careTest.failNext("InstallAppUpdate", "something else is still running - wait for it to finish"));
  await page.getByRole("button", { name: "Update now", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Your current version was kept");
  await expect(setup(page)).toBeEnabled();
  await expect(connect(page)).toBeEnabled();
  await expect(page.getByText("something else is still running - wait for it to finish")).toHaveCount(0);
});

test("failed checks are retryable and do not block navigation or assume every error is offline", async ({ page }) => {
  await visit(page);
  await page.evaluate(() => window.careTest.failNext("CheckAppUpdate", "couldn't reach GitHub to check for updates: dial tcp refused"));
  await page.getByRole("button", { name: "Check for updates", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("This computer seems to be offline.");
  await expect(connect(page)).toBeEnabled();
  await page.getByRole("button", { name: "Try again", exact: true }).click();
  await expect(page.getByText("Up to date", { exact: true })).toBeVisible();

  await page.evaluate(() => window.careTest.failNext("CheckAppUpdate", "GitHub answered HTTP 403 for https://api.github.com"));
  await page.getByRole("button", { name: "Check for updates", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Updates couldn't be checked right now.");
  await expect(page.getByText(/HTTP 403|seems to be offline/)).toHaveCount(0);
});

test("no installer can be dismissed without falsely claiming the current version is up to date", async ({ page }) => {
  await visit(page, "unavailable");
  await expect(page.getByRole("alert")).toContainText("New version, but not for this computer yet");
  await expect(page.getByText("0.1.6 is out for other systems. Check again another day.")).toBeVisible();
  await expect(setup(page)).toBeEnabled();
  await page.getByRole("button", { name: "Dismiss", exact: true }).click();
  await expect(page.getByText("Up to date", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Check for updates", exact: true })).toBeVisible();
});

test("release note and log-opening failures are visible and retryable", async ({ page }) => {
  await visit(page, "available");
  await page.evaluate(() => window.careTest.failNext("OpenURL", "native bridge private failure"));
  await page.getByRole("button", { name: "What's new", exact: true }).click();
  await expect(page.getByRole("alert")).toHaveText("Couldn't open what's new. Try again.");
  await expect(page.getByText("native bridge private failure")).toHaveCount(0);
  await page.getByRole("button", { name: "What's new", exact: true }).click();
  await expect(page.getByRole("alert")).toHaveCount(0);

  await page.getByRole("button", { name: "Update now", exact: true }).click();
  await page.evaluate(() => window.careTest.finishUpdate("failed"));
  await page.evaluate(() => window.careTest.failNext("OpenLogFolder", "no log path: /private/log"));
  await page.getByRole("button", { name: "Open log file", exact: true }).click();
  await expect(page.getByText("Couldn't open the log file. Try again, or ask the person who looks after this computer.")).toBeVisible();
  await expect(page.getByText(/private\/log/)).toHaveCount(0);
});

test("keyboard focus is visible and activates both paths", async ({ page }) => {
  await visit(page);
  await page.keyboard.press("Tab");
  await expect(page.getByRole("button", { name: "Check for updates", exact: true })).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(setup(page)).toBeFocused();
  expect(await setup(page).evaluate((element) => getComputedStyle(element).outlineStyle)).toBe("solid");
  await screenshot(page, "start-keyboard-focus");
  await page.keyboard.press("Tab");
  await expect(connect(page)).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: "Find your clinic's server" })).toBeVisible();
  await page.getByRole("button", { name: "Back", exact: true }).click();
  await setup(page).focus();
  await page.keyboard.press("Enter");
  await continueServerChecks(page);
  await expect(page.getByLabel("Clinic address", { exact: true })).toBeVisible();
});

test("the approved client and setup flows have separate scoped layouts", async ({ page }) => {
  await visit(page);
  await connect(page).click();
  await expect(page.getByRole("heading", { name: "Find your clinic's server" })).toBeVisible();
  await expect(page.locator(".start-brand-panel")).toHaveCount(0);
  const clientStyles = await page.getByRole("heading", { name: "Find your clinic's server" }).evaluate((element) => {
    const style = getComputedStyle(element);
    return { fontSize: style.fontSize, color: style.color };
  });
  expect(clientStyles).toEqual({ fontSize: "34px", color: "rgb(15, 26, 22)" });
  await screenshot(page, "onboarding-client");
  await page.getByRole("button", { name: "Back", exact: true }).click();
  await setup(page).click();
  await continueServerChecks(page);
  await expect(page.getByLabel("Clinic address", { exact: true })).toBeVisible();
  await expect(page.locator(".start-brand-panel")).toHaveCount(0);
  await expect(page.locator("aside")).toHaveCSS("width", "272px");
  await screenshot(page, "onboarding-setup");
});

for (const size of [{ width: 1100, height: 700 }, { width: 720, height: 560 }]) {
  for (const scenario of ["current", "available", "downloading", "verifying", "installing", "restarting", "installer", "failed", "download-failed", "offline", "unavailable"]) {
    test(`${scenario} fits ${size.width}x${size.height}`, async ({ page }) => {
      await page.setViewportSize(size);
      await visit(page, scenario);
      if (!["current", "available", "offline", "unavailable"].includes(scenario)) {
        await page.getByRole("button", { name: "Update now", exact: true }).click();
        await expect(page.getByRole("button", { name: "Update now", exact: true })).toHaveCount(0);
      }
      await page.evaluate(() => document.fonts.ready);
      const overflow = await page.evaluate(() => {
        const root = document.querySelector(".start-screen")!;
        return {
          pageHorizontal: document.documentElement.scrollWidth > innerWidth,
          pageVertical: document.documentElement.scrollHeight > innerHeight,
          horizontal: root.scrollWidth > root.clientWidth,
          vertical: root.scrollHeight > root.clientHeight,
          clipped: [...root.querySelectorAll("button, h1, h2, .start-update-card, .start-version-line")]
            .some((element) => {
              const rect = element.getBoundingClientRect();
              return rect.x < 0 || rect.y < 0 || rect.right > innerWidth + 1 || rect.bottom > innerHeight + 1;
            }),
        };
      });
      expect(overflow).toEqual({
        pageHorizontal: false, pageVertical: false, horizontal: false, vertical: false, clipped: false,
      });
      await screenshot(page, `start-${scenario}-${size.width}x${size.height}`);
    });
  }
}
