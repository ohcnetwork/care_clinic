import { expect, test, type Page } from "@playwright/test";
import type {} from "./fixtures/host";

const desktopPassword = "ClinicTest123";
const webPassword = "WardRounds-2026";
const root = (page: Page) => page.locator(".care-advanced");
const dialog = (page: Page) => page.getByRole("alertdialog");
const resetButton = (page: Page) => root(page).getByRole("button", { name: "Reset a CARE web password", exact: true });
const calls = (page: Page) => page.evaluate(() =>
  window.careTest.calls.filter((call) => call.method === "ResetWebPassword").map((call) => call.args));

test.use({ trace: "off" });
test.beforeEach(async ({ page }) => {
  await page.routeWebSocket("**", (socket) => socket.close());
});

async function openAdvanced(page: Page) {
  await page.goto("/tests/fixtures/index.html?scenario=panel-running");
  await page.waitForFunction(() => !!window.careTest);
  await page.getByRole("button", { name: "Advanced", exact: true }).click();
  await expect(root(page).getByLabel("CARE Clinic admin password", { exact: true })).toBeVisible();
}

async function unlock(page: Page) {
  await root(page).getByLabel("CARE Clinic admin password", { exact: true }).fill(desktopPassword);
  await root(page).getByRole("button", { name: "Unlock", exact: true }).click();
  await expect(resetButton(page)).toBeEnabled();
}

async function fill(page: Page, password: string, confirm = password) {
  await dialog(page).getByLabel("New CARE web password", { exact: true }).fill(password);
  await dialog(page).getByLabel("Confirm new CARE web password", { exact: true }).fill(confirm);
}

test("the web password reset stays hidden until Advanced is unlocked", async ({ page }) => {
  await openAdvanced(page);
  await expect(resetButton(page)).toHaveCount(0);
  await expect(root(page).getByText("CARE web sign-in", { exact: true })).toHaveCount(0);
  await unlock(page);
  await expect(root(page).getByRole("heading", { name: "CARE web sign-in", exact: true })).toBeVisible();
  await root(page).getByRole("button", { name: "Lock Advanced settings" }).click();
  await expect(resetButton(page)).toHaveCount(0);
  expect(await calls(page)).toEqual([]);
});

test("the dialog explains the reset, defaults to admin and blocks a mismatched confirmation", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await resetButton(page).click();
  await expect(dialog(page).getByLabel("CARE username", { exact: true })).toHaveValue("admin");
  await expect(dialog(page)).toContainText("password staff use to sign in to CARE in the browser");
  await expect(dialog(page)).toContainText("doesn't change the CARE Clinic admin password");
  await expect(dialog(page)).toContainText("CARE can't email or text password reset links");
  await expect(dialog(page)).toContainText("stays signed in until they sign out or their session expires");
  const submit = dialog(page).getByRole("button", { name: "Reset web password", exact: true });
  await expect(submit).toBeDisabled();
  await fill(page, webPassword, `${webPassword}x`);
  await expect(dialog(page)).toContainText("The passwords don't match");
  await expect(submit).toBeDisabled();
  await page.keyboard.press("Enter");
  await dialog(page).getByLabel("CARE username", { exact: true }).fill("");
  await fill(page, webPassword);
  await expect(submit).toBeDisabled();
  expect(await calls(page)).toEqual([]);
  await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(dialog(page)).toBeHidden();
});

test("CARE's password rules and unknown users are reported without closing the dialog", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await resetButton(page).click();
  await fill(page, "123");
  await dialog(page).getByRole("button", { name: "Reset web password", exact: true }).click();
  await expect(dialog(page)).toContainText("CARE didn't accept that password");
  await expect(dialog(page)).toContainText("This password is too short. It must contain at least 8 characters. This password is entirely numeric.");
  await expect(dialog(page).getByLabel("New CARE web password", { exact: true })).toHaveValue("");
  await expect(dialog(page).getByLabel("Confirm new CARE web password", { exact: true })).toHaveValue("");

  await dialog(page).getByLabel("CARE username", { exact: true }).fill("Nurse");
  await fill(page, webPassword);
  await dialog(page).getByRole("button", { name: "Reset web password", exact: true }).click();
  await expect(dialog(page)).toContainText("No CARE user has that username");
  await expect(dialog(page)).toContainText('There is no CARE user named "Nurse"');
  await expect(dialog(page).getByLabel("CARE username", { exact: true })).toHaveValue("Nurse");
  expect(await page.evaluate(() => window.careTest.fixtures.webUsers)).toEqual({ admin: "ClinicTest123", nurse: "NurseTest123" });
});

test("a successful reset sends the unlock password and names the user", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await resetButton(page).click();
  await dialog(page).getByLabel("CARE username", { exact: true }).fill(" nurse ");
  await fill(page, webPassword);
  await dialog(page).getByRole("button", { name: "Reset web password", exact: true }).click();
  await expect(dialog(page)).toBeHidden();
  await expect(root(page)).toContainText("CARE web password changed for nurse");
  await expect(root(page)).toContainText("The CARE Clinic admin password is unchanged.");
  expect(await calls(page)).toEqual([["nurse", webPassword, desktopPassword]]);
  expect(await page.evaluate(() => window.careTest.fixtures)).toMatchObject({
    adminPassword: desktopPassword, webUsers: { admin: "ClinicTest123", nurse: webPassword },
  });
  await resetButton(page).click();
  await expect(dialog(page).getByLabel("CARE username", { exact: true })).toHaveValue("admin");
  await expect(dialog(page).getByLabel("New CARE web password", { exact: true })).toHaveValue("");
});

test("a reset refused by the host is explained", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await resetButton(page).click();
  await page.evaluate(() => window.careTest.failNext("ResetWebPassword", "CARE isn't running; start CARE from Overview, then try again"));
  await fill(page, webPassword);
  await dialog(page).getByRole("button", { name: "Reset web password", exact: true }).click();
  await expect(dialog(page)).toContainText("Start CARE first");
  await page.evaluate(() => window.careTest.failNext("ResetWebPassword", "CARE couldn't change that web password; nothing was confirmed. Open the log file for details"));
  await fill(page, webPassword);
  await dialog(page).getByRole("button", { name: "Reset web password", exact: true }).click();
  await expect(dialog(page)).toContainText("The web password couldn't be changed");
  expect(await page.evaluate(() => window.careTest.fixtures.webUsers.admin)).toBe("ClinicTest123");
});
