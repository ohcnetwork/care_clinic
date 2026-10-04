import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { expect, test, type Page } from "@playwright/test";
import type {} from "./fixtures/host";
import { continueServerChecks } from "./helpers/setup";

const dialog = (page: Page) => page.getByRole("alertdialog");
const calls = (page: Page, method: string) => page.evaluate((name) =>
  window.careTest.calls.filter((call) => call.method === name), method);

test.beforeEach(async ({ page }) => {
  await page.routeWebSocket("**", (socket) => socket.close());
  page.on("pageerror", (error) => { throw error; });
});

async function panel(page: Page) {
  await page.goto("/tests/fixtures/index.html?scenario=panel-running");
  await expect(page.locator(".care-panel")).toBeVisible();
}

async function backup(page: Page) {
  await page.evaluate(() => { window.careTest.fixtures.finishJobs = false; });
  await page.getByRole("button", { name: "Back up now", exact: true }).click();
  await expect.poll(async () => (await calls(page, "ClinicAction")).length).toBe(1);
  await expect(page.getByRole("button", { name: "Back up now", exact: true })).toBeDisabled();
}

async function quit(page: Page, action = "backing up") {
  await page.evaluate((name) => window.careTest.requestQuit(name), action);
  await expect(dialog(page).getByRole("heading", { name: "CARE is still working", exact: true })).toBeVisible();
}

async function capture(page: Page, name: string) {
  if (!process.env.CARE_SCREENSHOTS_DIR) return;
  await mkdir(process.env.CARE_SCREENSHOTS_DIR, { recursive: true });
  await page.evaluate(() => document.fonts.ready);
  await page.screenshot({ path: join(process.env.CARE_SCREENSHOTS_DIR, `${name}.png`), animations: "disabled" });
}

async function permission(page: Page, title = "Set up care.local on this computer?",
  message = "This lets you open CARE securely on this computer.\n\nEnter your computer's administrator password and approve the security prompt when macOS asks.") {
  await page.evaluate(({ title, message }) => window.careTest.requestConfirmation(title, message), { title, message });
  await expect(dialog(page).getByRole("heading", { name: title, exact: true })).toBeVisible();
}

test.describe("permission confirmations", () => {
  const prompts = [
    { name: "setup", title: "Set up care.local on this computer?", message: "This lets you open CARE securely on this computer.\n\nEnter your computer's administrator password and approve the security prompt when macOS asks." },
    { name: "certificate", title: "Remove CARE's certificate?", message: "Remove CARE's security certificate from this computer. Approve the system permission prompt to continue." },
    { name: "address", title: "Remove CARE's saved address?", message: "Remove care.local from this computer. Approve the system permission prompt to continue." },
  ];
  for (const size of [{ width: 1100, height: 700 }, { width: 720, height: 560 }]) {
    for (const prompt of prompts) {
      test(`${prompt.name} is styled, concise, and fits ${size.width}x${size.height}`, async ({ page }) => {
        await page.setViewportSize(size);
        await panel(page);
        await page.getByRole("button", { name: "Advanced", exact: true }).click();
        await page.getByLabel("CARE Clinic admin password", { exact: true }).fill("ClinicTest123");
        await page.getByRole("button", { name: "Unlock", exact: true }).click();
        await expect(page.getByRole("button", { name: "Lock Advanced settings", exact: true })).toBeVisible();
        await permission(page, prompt.title, prompt.message);
        await expect(dialog(page).getByRole("button", { name: "Cancel", exact: true })).toBeFocused();
        await expect(dialog(page)).toHaveCSS("background-color", "rgb(255, 255, 255)");
        await expect(dialog(page)).not.toContainText("hosts file");
        await expect(dialog(page)).not.toContainText("keychain");
        const visibleMessage = await dialog(page).locator("p").first().innerText();
        expect(visibleMessage.trim().split(/\s+/).length).toBeLessThanOrEqual(35);
        expect(await dialog(page).evaluate((element) => {
          const box = element.getBoundingClientRect();
          return box.left >= 0 && box.right <= innerWidth && box.top >= 0 &&
            box.bottom <= innerHeight && element.scrollWidth <= element.clientWidth;
        })).toBe(true);
        await capture(page, `permission-${prompt.name}-${size.width}x${size.height}`);
        await dialog(page).getByRole("button", { name: "Continue", exact: true }).click();
        await expect(dialog(page)).toBeHidden();
        expect((await calls(page, "RespondToConfirmation")).map((call) => call.args)).toEqual([[1, true]]);
      });
    }
  }

  test("Escape declines permission without finishing the running job", async ({ page }) => {
    await panel(page);
    await backup(page);
    await permission(page);
    await page.keyboard.press("Escape");
    await expect(dialog(page)).toBeHidden();
    expect((await calls(page, "RespondToConfirmation")).map((call) => call.args)).toEqual([[1, false]]);
    await expect(page.getByRole("button", { name: "Back up now", exact: true })).toBeDisabled();
  });

  test("answers are single-flight and a stale response cannot close the next request", async ({ page }) => {
    await panel(page);
    await permission(page);
    await page.evaluate(() => window.careTest.hold("RespondToConfirmation"));
    await dialog(page).getByRole("button", { name: "Continue", exact: true }).click();
    await expect(dialog(page).getByRole("button", { name: "Cancel", exact: true })).toBeDisabled();
    await page.keyboard.press("Enter");
    expect(await calls(page, "RespondToConfirmation")).toHaveLength(1);
    await permission(page, "Remove CARE's saved address?", "Approve the system permission prompt to continue.");
    await page.evaluate(() => window.careTest.release("RespondToConfirmation"));
    await expect(dialog(page)).toContainText("Remove CARE's saved address?");
    await expect(dialog(page).getByRole("alert")).toHaveCount(0);
    await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(dialog(page)).toBeHidden();
    expect((await calls(page, "RespondToConfirmation")).map((call) => call.args)).toEqual([[1, true], [2, false]]);
  });

  test("response and recheck errors are friendly and retryable", async ({ page }) => {
    await panel(page);
    await permission(page);
    await page.evaluate(() => window.careTest.failNext("RespondToConfirmation", "private permission transport detail"));
    await dialog(page).getByRole("button", { name: "Continue", exact: true }).click();
    await expect(dialog(page).getByRole("alert")).toContainText("Your answer couldn't be sent");
    await expect(dialog(page)).not.toContainText("private permission");
    await page.evaluate(() => window.careTest.failNext("SetConfirmationDialogReady", "private permission snapshot"));
    await dialog(page).getByRole("button", { name: "Check again", exact: true }).click();
    await expect(dialog(page).getByRole("alert")).toContainText("couldn't be checked");
    await dialog(page).getByRole("button", { name: "Check again", exact: true }).click();
    await expect(dialog(page).getByRole("alert")).toHaveCount(0);
    await dialog(page).getByRole("button", { name: "Continue", exact: true }).click();
    await expect(dialog(page)).toBeHidden();
  });

  test("a delayed registration and stale events cannot revive cancelled permission", async ({ page }) => {
    await page.addInitScript(() => {
      Object.defineProperty(window, "careTest", {
        configurable: true,
        set(host: Window["careTest"]) {
          Object.defineProperty(window, "careTest", { value: host, configurable: true });
          host.hold("SetConfirmationDialogReady");
          host.respond("SetConfirmationDialogReady", null);
        },
      });
    });
    await panel(page);
    await permission(page);
    await page.evaluate(() => {
      window.careTest.cancelConfirmation();
      window.careTest.emit("confirmation-requested", { id: 1, title: "Expired request", message: "This must stay closed." });
      window.careTest.release("SetConfirmationDialogReady");
    });
    await expect(dialog(page)).toBeHidden();
    await permission(page, "New permission", "This is the current request.");
    await page.evaluate(() => window.careTest.emit("confirmation-cancelled", 1));
    await expect(dialog(page)).toContainText("New permission");
    await dialog(page).getByRole("button", { name: "Continue", exact: true }).click();
    expect((await calls(page, "RespondToConfirmation")).map((call) => call.args)).toEqual([[2, true]]);
  });

  test("keeping the app open returns to the pending permission request", async ({ page }) => {
    await panel(page);
    await backup(page);
    await permission(page);
    await quit(page);
    await dialog(page).getByRole("button", { name: "Keep waiting", exact: true }).click();
    await expect(dialog(page)).toContainText("Set up care.local on this computer?");
    expect(await calls(page, "RespondToConfirmation")).toHaveLength(0);
    await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(dialog(page)).toBeHidden();
  });
});

for (const size of [{ width: 1100, height: 700 }, { width: 720, height: 560 }]) {
  test(`busy quit defaults to waiting and fits ${size.width}x${size.height}`, async ({ page }) => {
    await page.setViewportSize(size);
    await panel(page);
    await backup(page);
    await quit(page);
    const keepWaiting = dialog(page).getByRole("button", { name: "Keep waiting", exact: true });
    await expect(keepWaiting).toBeFocused();
    expect(await dialog(page).evaluate((element) => {
      const bounds = element.getBoundingClientRect();
      return bounds.left >= 0 && bounds.right <= innerWidth && bounds.top >= 0 &&
        bounds.bottom <= innerHeight && element.scrollWidth <= element.clientWidth;
    })).toBe(true);
    await capture(page, `quit-busy-${size.width}x${size.height}`);
    await page.keyboard.press("Enter");
    await expect(dialog(page)).toBeHidden();
    expect((await calls(page, "RespondToQuit")).map((call) => call.args)).toEqual([[1, false]]);
    expect(await page.evaluate(() => window.careTest.fixtures.quitAccepted)).toBe(false);
    await expect(page.getByRole("button", { name: "Back up now", exact: true })).toBeDisabled();
    await page.evaluate(() => window.careTest.finishJob("backup-now"));
    await expect(page.getByRole("button", { name: "Back up now", exact: true })).toBeEnabled();
  });
}

test("Escape cancels the close request without stopping the accepted job", async ({ page }) => {
  await panel(page);
  await backup(page);
  await quit(page);
  await page.keyboard.press("Escape");
  await expect(dialog(page)).toBeHidden();
  expect((await calls(page, "RespondToQuit")).map((call) => call.args)).toEqual([[1, false]]);
  await expect(page.getByRole("button", { name: "Back up now", exact: true })).toBeDisabled();
  expect((await calls(page, "ClinicAction")).map((call) => call.args[0])).toEqual(["backup-now"]);
});

test("quit confirmation is single-flight and uses the native request ID", async ({ page }) => {
  await panel(page);
  await quit(page, "installing CARE");
  await page.evaluate(() => window.careTest.hold("RespondToQuit"));
  await dialog(page).getByRole("button", { name: "Quit anyway", exact: true }).click();
  await expect(dialog(page).getByRole("button", { name: "Keep waiting", exact: true })).toBeDisabled();
  await page.keyboard.press("Enter");
  expect((await calls(page, "RespondToQuit")).map((call) => call.args)).toEqual([[1, true]]);
  expect(await page.evaluate(() => window.careTest.fixtures.quitAccepted)).toBe(false);
  await page.evaluate(() => window.careTest.release("RespondToQuit"));
  await expect(dialog(page)).toBeHidden();
  expect(await page.evaluate(() => window.careTest.fixtures.quitAccepted)).toBe(true);
});

test("quit response and recheck failures stay visible and retryable without raw details", async ({ page }) => {
  await panel(page);
  await quit(page);
  await page.evaluate(() => window.careTest.failNext("RespondToQuit", "private runtime diagnostic"));
  await dialog(page).getByRole("button", { name: "Quit anyway", exact: true }).click();
  await expect(dialog(page).getByRole("alert")).toContainText("couldn't be completed");
  await expect(dialog(page)).not.toContainText("private runtime");
  expect(await page.evaluate(() => window.careTest.fixtures.quitAccepted)).toBe(false);
  await page.evaluate(() => window.careTest.failNext("SetQuitDialogReady", "private request diagnostic"));
  await dialog(page).getByRole("button", { name: "Check again", exact: true }).click();
  await expect(dialog(page).getByRole("alert")).toContainText("couldn't be checked");
  await expect(dialog(page)).not.toContainText("private request");
  await dialog(page).getByRole("button", { name: "Check again", exact: true }).click();
  await expect(dialog(page).getByRole("alert")).toHaveCount(0);
  await dialog(page).getByRole("button", { name: "Keep waiting", exact: true }).click();
  await expect(dialog(page)).toBeHidden();
});

test("a stale response cannot dismiss a newer close request", async ({ page }) => {
  await panel(page);
  await quit(page);
  await page.evaluate(() => window.careTest.hold("RespondToQuit"));
  await dialog(page).getByRole("button", { name: "Keep waiting", exact: true }).click();
  await quit(page, "restoring");
  await page.evaluate(() => window.careTest.release("RespondToQuit"));
  await expect(dialog(page)).toContainText("still restoring");
  await expect(dialog(page).getByRole("alert")).toHaveCount(0);
  await dialog(page).getByRole("button", { name: "Keep waiting", exact: true }).click();
  await expect(dialog(page)).toBeHidden();
  expect((await calls(page, "RespondToQuit")).map((call) => call.args)).toEqual([[1, false], [2, false]]);
});

test("a delayed registration snapshot cannot hide a newer close request", async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(window, "careTest", {
      configurable: true,
      set(preview: Window["careTest"]) {
        Object.defineProperty(window, "careTest", { value: preview, configurable: true });
        preview.hold("SetQuitDialogReady");
        preview.respond("SetQuitDialogReady", null);
      },
    });
  });
  await panel(page);
  await quit(page);
  await page.evaluate(() => window.careTest.release("SetQuitDialogReady"));
  await expect(dialog(page)).toContainText("still backing up");
  await dialog(page).getByRole("button", { name: "Keep waiting", exact: true }).click();
  await expect(dialog(page)).toBeHidden();
});

test("unavailable quit registration shows a friendly error rather than failing silently", async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(window, "careTest", {
      configurable: true,
      set(preview: Window["careTest"]) {
        Object.defineProperty(window, "careTest", { value: preview, configurable: true });
        window.go.main.App.SetQuitDialogReady = async () => {
          throw new Error("private registration diagnostic");
        };
      },
    });
  });
  await panel(page);
  const notice = page.locator("[data-sonner-toast][data-type=error]");
  await expect(notice).toContainText("The close request couldn't be checked");
  await expect(notice).not.toContainText("private registration");
});

for (const first of ["uninstalled", "care-done"] as const) {
  test(`removal returns to Start only after both events, with ${first} first`, async ({ page }) => {
    await panel(page);
    await page.getByRole("button", { name: "Advanced", exact: true }).click();
    await page.getByLabel("CARE Clinic admin password", { exact: true }).fill("ClinicTest123");
    await page.getByRole("button", { name: "Unlock", exact: true }).click();
    await page.getByRole("button", { name: "Uninstall…", exact: true }).click();
    await page.evaluate(() => { window.careTest.fixtures.finishJobs = false; });
    await dialog(page).getByLabel("Type DELETE to confirm", { exact: true }).fill("DELETE");
    await dialog(page).getByRole("button", { name: "Delete everything", exact: true }).click();
    await expect(dialog(page)).toBeHidden();
    const progress = page.getByRole("status", { name: "Removing CARE from this computer", exact: true });
    await expect(progress).toBeVisible();
    await expect(progress).toContainText("Please wait and keep this window open");
    await page.evaluate((event) => {
      window.careTest.state.role = "";
      window.careTest.state.setup_done = false;
      if (event === "uninstalled") window.careTest.emit("uninstalled", true);
      else window.careTest.emit("care-done", 0, "uninstall");
    }, first);
    await expect(progress).toBeVisible();
    await expect(page.locator(".care-advanced")).toBeVisible();
    await expect(page.getByRole("button", { name: "Uninstall…", exact: true, includeHidden: true })).toBeDisabled();
    await page.evaluate((event) => {
      if (event === "uninstalled") window.careTest.emit("care-done", 0, "uninstall");
      else window.careTest.emit("uninstalled", true);
    }, first);
    await expect(progress).toBeHidden();
    await expect(page.getByRole("heading", { name: "Set up CARE on this computer", exact: true })).toBeVisible();
    expect(await calls(page, "RemoveApp")).toHaveLength(0);
    await page.getByRole("button", { name: "Start setup", exact: true }).click();
    await continueServerChecks(page);
    await expect(page.getByLabel("Clinic address", { exact: true })).toHaveValue("care");
  });
}

for (const size of [{ width: 1100, height: 700 }, { width: 720, height: 560 }]) {
  test(`central removal progress cannot be dismissed and permits native confirmation at ${size.width}x${size.height}`, async ({ page }) => {
    await page.setViewportSize(size);
    await panel(page);
    await page.getByRole("button", { name: "Advanced", exact: true }).click();
    await page.getByLabel("Clinic admin password", { exact: true }).fill("ClinicTest123");
    await page.getByRole("button", { name: "Unlock", exact: true }).click();
    await page.getByRole("button", { name: "Uninstall…", exact: true }).click();
    await page.evaluate(() => {
      window.careTest.fixtures.finishJobs = false;
      window.careTest.hold("RunUninstall");
    });
    await dialog(page).getByLabel("Type DELETE to confirm", { exact: true }).fill("DELETE");
    await dialog(page).getByRole("button", { name: "Delete everything", exact: true }).click();
    const progress = page.getByRole("status", { name: "Removing CARE from this computer", exact: true });
    await expect(progress).toBeVisible();
    await expect(dialog(page)).toBeHidden();
    expect(await progress.evaluate((element) => {
      const box = element.getBoundingClientRect();
      return Math.abs((box.left + box.right) / 2 - innerWidth / 2) < 2 &&
        Math.abs((box.top + box.bottom) / 2 - innerHeight / 2) < 2 &&
        box.left >= 0 && box.right <= innerWidth && box.top >= 0 && box.bottom <= innerHeight;
    })).toBe(true);
    await page.keyboard.press("Escape");
    await page.mouse.click(5, 5);
    await expect(progress).toBeVisible();
    await permission(page, "Remove CARE's certificate?", "Approve the system permission prompt to continue.");
    await expect(dialog(page)).toHaveCount(1);
    await expect(dialog(page).getByRole("button", { name: "Cancel", exact: true })).toBeFocused();
    await page.keyboard.press("Tab");
    expect(await page.evaluate(() => !!document.activeElement?.closest('[role="alertdialog"]'))).toBe(true);
    await page.evaluate(() => window.careTest.failNext("RespondToConfirmation", "private permission diagnostic"));
    await dialog(page).getByRole("button", { name: "Continue", exact: true }).click();
    await expect(dialog(page).getByRole("alert")).toContainText("Your answer couldn't be sent");
    await dialog(page).getByRole("button", { name: "Continue", exact: true }).click();
    await expect(dialog(page)).toBeHidden();
    await expect(progress).toBeVisible();
    await page.evaluate(() => window.careTest.release("RunUninstall"));
    await expect(progress).toBeVisible();
    await page.evaluate(() => window.careTest.emit("care-error", "Removal needs attention", "certificate cleanup failed"));
    await expect(progress.getByRole("alert")).toContainText("Removal needs attention");
    await page.evaluate(() => window.careTest.finishJob("uninstall", "certificate cleanup failed"));
    await expect(progress).toBeHidden();
    await expect(page.getByRole("alert").filter({ hasText: "Removal didn't finish" }).first()).toBeVisible();
    await expect(page.getByRole("button", { name: "Uninstall…", exact: true })).toBeEnabled();
    expect(await calls(page, "RemoveApp")).toHaveLength(0);
  });
}

test("removal progress survives the optional app handoff and clears on its failure", async ({ page }) => {
  await panel(page);
  await page.evaluate(() => {
    window.careTest.fixtures.canRemoveApp = true;
    window.careTest.fixtures.finishJobs = false;
    window.careTest.hold("RemoveApp");
    window.careTest.failNext("RemoveApp", "private removal diagnostic");
  });
  await page.getByRole("button", { name: "Advanced", exact: true }).click();
  await page.getByLabel("Clinic admin password", { exact: true }).fill("ClinicTest123");
  await page.getByRole("button", { name: "Unlock", exact: true }).click();
  await page.getByRole("button", { name: "Uninstall…", exact: true }).click();
  await dialog(page).getByRole("checkbox", { name: /Also remove the CARE Clinic app/ }).check();
  await dialog(page).getByLabel("Type DELETE to confirm", { exact: true }).fill("DELETE");
  await dialog(page).getByRole("button", { name: "Delete everything", exact: true }).click();
  await page.evaluate(() => window.careTest.finishJob("uninstall"));
  const progress = page.getByRole("status", { name: "Removing CARE Clinic", exact: true });
  await expect(progress).toBeVisible();
  await expect(page.getByRole("button", { name: "Start setup", exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Start setup", exact: true }).locator("xpath=ancestor::*[@inert]")).toHaveCount(1);
  await page.keyboard.press("Escape");
  await expect(progress).toBeVisible();
  await page.evaluate(() => window.careTest.release("RemoveApp"));
  await expect(progress).toBeHidden();
  await expect(page.getByRole("button", { name: "Start setup", exact: true })).toBeEnabled();
  await expect(page.locator("[data-sonner-toast][data-type=error]")).toContainText("couldn't remove itself");
});

test("Windows clinic removal keeps progress through the native exit handoff and reports exit failure", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?screen=remove&platform=windows");
  await page.getByLabel("Clinic admin password", { exact: true }).fill("ClinicTest123");
  await page.getByRole("button", { name: "Unlock", exact: true }).click();
  await page.getByRole("button", { name: "Uninstall…", exact: true }).click();
  await page.evaluate(() => {
    window.careTest.fixtures.finishJobs = false;
    window.careTest.hold("ExitUninstall");
    window.careTest.failNext("ExitUninstall", "private exit diagnostic");
  });
  await dialog(page).getByLabel("Type DELETE to confirm", { exact: true }).fill("DELETE");
  await dialog(page).getByRole("button", { name: "Delete everything", exact: true }).click();
  const progress = page.getByRole("status", { name: "Removing CARE from this computer", exact: true });
  await expect(progress).toBeVisible();
  await page.evaluate(() => window.careTest.emit("care-done", 0, "uninstall"));
  await expect(progress).toBeVisible();
  expect(await calls(page, "ExitUninstall")).toHaveLength(0);
  await page.evaluate(() => window.careTest.emit("uninstalled", true));
  await expect.poll(async () => (await calls(page, "ExitUninstall")).length).toBe(1);
  await expect(progress).toBeVisible();
  await page.evaluate(() => window.careTest.release("ExitUninstall"));
  await expect(progress).toBeHidden();
  await expect(page.locator("[data-sonner-toast][data-type=error]")).toContainText("uninstaller couldn't close");
  expect(await calls(page, "RemoveApp")).toHaveLength(0);
});

async function installDemo(page: Page, fail = false) {
  await page.goto(`/tests/fixtures/index.html?simulateInstall=1${fail ? "&scenario=installation-failed" : ""}`);
  await page.getByRole("button", { name: "Start setup", exact: true }).click();
  await continueServerChecks(page);
  await page.getByLabel("Clinic address", { exact: true }).fill("preview-clinic");
  await page.getByRole("button", { name: "Continue", exact: true }).click();
  await page.getByRole("button", { name: "Change folder", exact: true }).click();
  await page.getByRole("button", { name: "Choose where to save", exact: true }).click();
  await page.getByRole("button", { name: "Select saved file", exact: true }).click();
  await page.getByRole("button", { name: "Continue", exact: true }).click();
  await page.getByLabel("Password", { exact: true }).fill("TestChosen789");
  await page.getByLabel("Confirm password", { exact: true }).fill("TestChosen789");
  await page.getByRole("button", { name: "Choose where to save", exact: true }).click();
  await page.getByRole("button", { name: "Continue", exact: true }).click();
  await page.getByRole("button", { name: "Install", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Installing CARE", exact: true })).toBeVisible();
}

test("the safe full demo reaches Overview and retains the chosen CARE Clinic password", async ({ page }) => {
  await installDemo(page);
  await expect(page.locator(".care-panel")).toBeVisible({ timeout: 15_000 });
  await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
  expect(await calls(page, "SelectRole")).toHaveLength(0);
  expect(await calls(page, "BeginServerSetup")).toHaveLength(1);
  expect(await calls(page, "RunSetup")).toHaveLength(1);
  await page.getByRole("button", { name: "Advanced", exact: true }).click();
  await page.getByLabel("CARE Clinic admin password", { exact: true }).fill("TestChosen789");
  await page.getByRole("button", { name: "Unlock", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Clinic settings", exact: true })).toBeVisible();
});

test("the simulated failed install retries setup without silently starting another installation", async ({ page }) => {
  await installDemo(page, true);
  await expect(page.getByRole("heading", { name: "Something went wrong during installation", exact: true })).toBeVisible({ timeout: 15_000 });
  await page.getByRole("button", { name: "Try again", exact: true }).click();
  await continueServerChecks(page);
  await expect(page.getByLabel("Clinic address", { exact: true })).toHaveValue("preview-clinic");
  expect(await calls(page, "CleanupFailedInstall")).toHaveLength(1);
  expect(await calls(page, "RunSetup")).toHaveLength(1);
  expect(await page.evaluate(() => window.careTest.fixtures.recovery.backup_verified)).toBe(false);
  expect(await page.evaluate(() => window.careTest.fixtures.recovery.codes_saved)).toBe(false);
});

test("Desktop installer handoff guards mutations across the entire panel", async ({ page }) => {
  await panel(page);
  await page.evaluate(() => {
    window.careTest.progress({ phase: "installer", done: 0, total: 0 });
    window.careTest.finishUpdate();
  });
  await expect(page.getByRole("button", { name: "Back up now", exact: true })).toBeDisabled();
  await expect(page.getByRole("switch")).toBeDisabled();
  await page.getByRole("button", { name: "Backups", exact: true }).click();
  await expect(page.getByRole("button", { name: "Back up now", exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Change folder", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Advanced", exact: true }).click();
  await expect(page.getByLabel("CARE Clinic admin password", { exact: true })).toBeDisabled();
  await page.evaluate(() => window.careTest.emit("care-done", 0, "status"));
  await expect(page.getByLabel("CARE Clinic admin password", { exact: true })).toBeDisabled();
  expect(await calls(page, "WriteEnv")).toHaveLength(0);
  expect(await calls(page, "RunUninstall")).toHaveLength(0);
  expect(await calls(page, "SetBackupDir")).toHaveLength(0);
  expect(await calls(page, "ClinicAction")).toHaveLength(0);
});

test("installer acknowledgement releases the guard only after native completion", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?scenario=available");
  await page.getByRole("button", { name: "Update now", exact: true }).click();
  await page.evaluate(() => window.careTest.progress({ phase: "installer", done: 0, total: 0 }));
  const acknowledge = page.getByRole("button", { name: "OK", exact: true });
  await expect(acknowledge).toBeDisabled();
  await page.evaluate(() => window.careTest.finishUpdate());
  await expect(acknowledge).toBeEnabled();
  await expect(page.getByRole("button", { name: "Start setup", exact: true })).toBeDisabled();
  await acknowledge.click();
  await expect(page.getByRole("button", { name: "Start setup", exact: true })).toBeEnabled();
  await expect(page.getByText("Up to date", { exact: true })).toHaveCount(0);
  expect(await calls(page, "InstallAppUpdate")).toHaveLength(1);
});

test("failed optional app removal leaves Start usable and explains what remains", async ({ page }) => {
  await panel(page);
  await page.evaluate(() => {
    window.careTest.fixtures.canRemoveApp = true;
    window.careTest.fixtures.finishJobs = false;
    window.careTest.failNext("RemoveApp", "private removal diagnostic");
  });
  await page.getByRole("button", { name: "Advanced", exact: true }).click();
  await page.getByLabel("CARE Clinic admin password", { exact: true }).fill("ClinicTest123");
  await page.getByRole("button", { name: "Unlock", exact: true }).click();
  await page.getByRole("button", { name: "Uninstall…", exact: true }).click();
  await dialog(page).getByRole("checkbox", { name: /Also remove the CARE Clinic app/ }).check();
  await dialog(page).getByLabel("Type DELETE to confirm", { exact: true }).fill("DELETE");
  await dialog(page).getByRole("button", { name: "Delete everything", exact: true }).click();
  await page.evaluate(() => window.careTest.finishJob("uninstall"));
  await expect(page.getByRole("button", { name: "Start setup", exact: true })).toBeEnabled();
  const error = page.locator("[data-sonner-toast][data-type=error]");
  await expect(error).toContainText("The clinic was removed, but CARE Clinic couldn't remove itself");
  await expect(error).not.toContainText("private removal");
  expect(await page.evaluate(() => window.careTest.fixtures.appRemoved)).toBe(false);
  expect(await calls(page, "RemoveApp")).toHaveLength(1);
});
