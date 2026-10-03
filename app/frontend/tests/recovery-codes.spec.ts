import { expect, test, type Page } from "@playwright/test";
import type {} from "./fixtures/host";

const password = "ClinicTest123";
const reminder = (page: Page) => page.getByLabel("Recovery codes reminder", { exact: true });
const modal = (page: Page) => page.getByRole("alertdialog");
const saveButton = (page: Page) => modal(page).getByRole("button", { name: "Choose where to save", exact: true });

test.beforeEach(async ({ page }) => {
  await page.routeWebSocket("**", (socket) => socket.close());
  page.on("pageerror", (error) => { throw error; });
});

async function panel(page: Page, remaining: number) {
  await page.goto(`/tests/fixtures/index.html?scenario=panel-running&recoveryCodes=${remaining}`);
  await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
}

for (const remaining of [0, 1, 2, 3, 6]) {
  test(`recovery reminder threshold and persistence with ${remaining} codes`, async ({ page }) => {
    await panel(page, remaining);
    await expect.poll(() => page.evaluate(() => window.careTest.calls.some((call) => call.method === "GetAdminRecoveryCodeCount"))).toBe(true);
    if (remaining > 2) {
      await expect(reminder(page)).toHaveCount(0);
      return;
    }
    const title = remaining === 0 ? "No recovery codes left." : `Only ${remaining} recovery ${remaining === 1 ? "code" : "codes"} left.`;
    await expect(reminder(page)).toContainText(title);
    await expect(reminder(page)).toHaveCSS("color", remaining === 0 ? "rgb(153, 27, 27)" : "rgb(146, 64, 14)");
    await page.getByRole("navigation", { name: "Clinic sections" }).getByRole("button", { name: "Backups", exact: true }).click();
    await expect(reminder(page)).toContainText(title);
    await page.reload();
    await expect(reminder(page)).toContainText(title);
    await expect(reminder(page).getByRole("button", { name: /dismiss|later/i })).toHaveCount(0);
  });
}

test("banner requires a password and stays through cancellation and failed saves", async ({ page }) => {
  await panel(page, 2);
  await reminder(page).getByRole("button", { name: "Save new recovery codes" }).click();
  await expect(modal(page)).toContainText("replaces all old codes");
  await expect(saveButton(page)).toBeDisabled();
  await modal(page).getByLabel("Desktop admin password", { exact: true }).fill("wrong");
  await saveButton(page).click();
  await expect(modal(page)).toContainText("The Desktop admin password didn't match");
  await expect(modal(page).getByLabel("Desktop admin password", { exact: true })).toHaveValue("");
  await expect(reminder(page)).toContainText("Only 2 recovery codes left.");

  await page.evaluate(() => window.careTest.respond("SaveAdminRecoveryCodes", false));
  await modal(page).getByLabel("Desktop admin password", { exact: true }).fill(password);
  await saveButton(page).click();
  await expect(modal(page)).toContainText("No new recovery codes were saved");
  await expect(reminder(page)).toBeVisible();
  await page.evaluate(() => window.careTest.failNext("SaveAdminRecoveryCodes", "could not activate the new recovery codes; keep the previous sheet and retry: private error"));
  await modal(page).getByLabel("Desktop admin password", { exact: true }).fill(password);
  await saveButton(page).click();
  await expect(modal(page)).toContainText("The new codes weren't activated");
  await expect(reminder(page)).toBeVisible();
  await modal(page).getByRole("button", { name: "Cancel", exact: true }).click();
  await reminder(page).getByRole("button", { name: "Save new recovery codes" }).click();
  await expect(modal(page).getByLabel("Desktop admin password", { exact: true })).toHaveValue("");
});

test("successful replacement clears the reminder only after the native save completes", async ({ page }) => {
  await panel(page, 0);
  await reminder(page).getByRole("button", { name: "Save new recovery codes" }).click();
  await modal(page).getByLabel("Desktop admin password", { exact: true }).fill(password);
  await page.evaluate(() => window.careTest.hold("SaveAdminRecoveryCodes"));
  await saveButton(page).click();
  await expect(modal(page).getByRole("button", { name: "Cancel", exact: true })).toBeDisabled();
  await expect(reminder(page)).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(modal(page)).toBeVisible();
  await page.evaluate(() => window.careTest.release("SaveAdminRecoveryCodes"));
  await expect(modal(page)).toHaveCount(0);
  await expect(reminder(page)).toHaveCount(0);
  expect(await page.evaluate(() => window.careTest.fixtures.recoveryCodes.filter(Boolean).length)).toBe(6);
});

test("using a recovery code updates the banner and Advanced replacement clears it", async ({ page }) => {
  await panel(page, 3);
  await page.getByRole("button", { name: "Advanced", exact: true }).click();
  await page.getByRole("button", { name: "Forgot Desktop password?", exact: true }).click();
  const code = await page.evaluate(() => window.careTest.fixtures.recoveryCodes[0]);
  await page.getByLabel("Unused recovery code", { exact: true }).fill(code);
  await page.getByLabel("New Desktop admin password", { exact: true }).fill("ClinicChanged456");
  await page.getByLabel("Confirm new Desktop admin password", { exact: true }).fill("ClinicChanged456");
  await page.getByRole("button", { name: "Reset Desktop password", exact: true }).click();
  await expect(reminder(page)).toContainText("Only 2 recovery codes left.");
  await page.getByRole("button", { name: "New recovery codes", exact: true }).click();
  await saveButton(page).click();
  await expect(modal(page)).toHaveCount(0);
  await expect(reminder(page)).toHaveCount(0);
});

test("an app update blocks an open recovery dialog and clears its password", async ({ page }) => {
  await panel(page, 1);
  await reminder(page).getByRole("button", { name: "Save new recovery codes" }).click();
  await modal(page).getByLabel("Desktop admin password", { exact: true }).fill(password);
  await page.evaluate(() => window.careTest.progress({ phase: "verifying", done: 1, total: 1 }));
  await expect(modal(page).getByLabel("Desktop admin password", { exact: true })).toHaveValue("");
  await expect(saveButton(page)).toBeDisabled();
  await expect(reminder(page)).toBeVisible();
  expect(await page.evaluate(() => window.careTest.calls.filter((call) => call.method === "SaveAdminRecoveryCodes").length)).toBe(0);
});

test("the recovery banner and save dialog fit a small window", async ({ page }) => {
  await page.setViewportSize({ width: 720, height: 560 });
  await panel(page, 0);
  await expect(reminder(page)).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await reminder(page).getByRole("button", { name: "Save new recovery codes" }).click();
  await expect(modal(page)).toBeVisible();
  const bounds = await modal(page).boundingBox();
  expect(bounds).not.toBeNull();
  expect(bounds!.x).toBeGreaterThanOrEqual(0);
  expect(bounds!.y).toBeGreaterThanOrEqual(0);
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(720);
  expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(560);
});
