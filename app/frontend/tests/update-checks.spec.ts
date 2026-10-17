import { expect, test, type Page } from "@playwright/test";
import type {} from "./fixtures/host";

const password = "ClinicTest123";
const calls = (page: Page, method: Parameters<Window["careTest"]["failNext"]>[0]) => page.evaluate((name) =>
  window.careTest.calls.filter((call) => call.method === name), method);
const overview = (page: Page) => page.locator('.panel-page[aria-label="Overview"]');
const updates = (page: Page) => page.locator('.panel-page[aria-label="Updates"]');
const advanced = (page: Page) => page.locator(".care-advanced");
const section = (page: Page, name: string) => page.getByRole("navigation", { name: "Clinic sections" })
  .getByRole("button", { name: new RegExp(`^${name}`) });
const software = (page: Page) => page.getByRole("region", { name: "CARE software", exact: true });
const desktop = (page: Page) => page.getByRole("region", { name: "CARE Clinic application", exact: true });
const appSwitch = (page: Page) => advanced(page).getByRole("switch", { name: "Check for CARE Clinic app updates", exact: true });
const careSwitch = (page: Page) => advanced(page).getByRole("switch", { name: "Check for CARE software updates", exact: true });
const careBanner = (page: Page) => page.getByText("A CARE software update is ready to install.", { exact: true });

test.use({ trace: "off" });
test.beforeEach(async ({ page }) => {
  await page.routeWebSocket("**", (socket) => socket.close());
});

async function openPanel(page: Page, query: string) {
  await page.goto(`/tests/fixtures/index.html?${query}`);
  await expect(overview(page).getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
}

async function unlockAdvanced(page: Page) {
  await section(page, "Advanced").click();
  await advanced(page).getByLabel("CARE Clinic admin password", { exact: true }).fill(password);
  await advanced(page).getByRole("button", { name: "Unlock", exact: true }).click();
  await expect(advanced(page).getByRole("heading", { name: "Update checks", exact: true })).toBeVisible();
}

test("Advanced turns each kind of update check off and on with the admin password", async ({ page }) => {
  await openPanel(page, "scenario=available&screen=panel");
  await unlockAdvanced(page);
  await expect(appSwitch(page)).toBeChecked();
  await expect(careSwitch(page)).toBeChecked();
  await expect(advanced(page)).toContainText("never contacts GitHub");

  await appSwitch(page).click();
  await expect(appSwitch(page)).not.toBeChecked();
  await expect(careSwitch(page)).toBeChecked();
  expect((await calls(page, "SetUpdateChecks")).map((call) => call.args)).toEqual([[false, true, password]]);
  expect(await page.evaluate(() => window.careTest.fixtures.updateChecks)).toEqual({ app: false, care: true });

  await careSwitch(page).click();
  await expect(careSwitch(page)).not.toBeChecked();
  expect(await page.evaluate(() => window.careTest.fixtures.updateChecks)).toEqual({ app: false, care: false });

  await appSwitch(page).click();
  await careSwitch(page).click();
  await expect(appSwitch(page)).toBeChecked();
  await expect(careSwitch(page)).toBeChecked();
  expect(await page.evaluate(() => window.careTest.fixtures.updateChecks)).toEqual({ app: true, care: true });
});

test("a rejected password leaves update checks unchanged", async ({ page }) => {
  await openPanel(page, "scenario=panel-current");
  await unlockAdvanced(page);
  await page.evaluate(() => { window.careTest.fixtures.adminPassword = "SomethingElse789"; });
  await careSwitch(page).click();
  await expect(advanced(page)).toContainText("The CARE Clinic admin password didn't match");
  await expect(careSwitch(page)).toBeChecked();
  expect(await page.evaluate(() => window.careTest.fixtures.updateChecks)).toEqual({ app: true, care: true });
});

test("Updates says checks can be turned off in Advanced while both are on", async ({ page }) => {
  await openPanel(page, "scenario=panel-current");
  await section(page, "Updates").click();
  const notice = updates(page).getByRole("status").filter({ hasText: "Automatic update checks are on" });
  await expect(notice).toContainText("An administrator can turn either check off in Advanced.");
  await expect(updates(page)).not.toContainText("checks are turned off");
  await expect(software(page).getByRole("button", { name: "Check CARE software updates" })).toBeVisible();
  await expect(desktop(page).getByRole("button", { name: "Check CARE Clinic updates" })).toBeVisible();
  await notice.getByRole("button", { name: "Open Advanced", exact: true }).click();
  await expect(advanced(page).getByLabel("CARE Clinic admin password", { exact: true })).toBeVisible();
});

test("app update checks off: no GitHub check, no controls, and a way back to Advanced", async ({ page }) => {
  await openPanel(page, "scenario=available&screen=panel&checksOff=app");
  await section(page, "Updates").click();
  await expect(updates(page).getByRole("status").filter({ hasText: "CARE Clinic app update checks are turned off" })).toBeVisible();
  await expect(updates(page)).toContainText("CARE software updates still work.");
  await expect(desktop(page)).toContainText("Checks off");
  await expect(desktop(page).getByRole("button", { name: "Check CARE Clinic updates" })).toHaveCount(0);
  await expect(desktop(page).getByRole("button", { name: "Update CARE Clinic", exact: true })).toHaveCount(0);
  await expect(software(page).getByRole("button", { name: "Check CARE software updates" })).toBeVisible();
  expect(await calls(page, "CheckAppUpdate")).toHaveLength(0);

  await updates(page).getByRole("button", { name: "Open Advanced", exact: true }).click();
  await expect(advanced(page).getByLabel("CARE Clinic admin password", { exact: true })).toBeVisible();
  await advanced(page).getByLabel("CARE Clinic admin password", { exact: true }).fill(password);
  await advanced(page).getByRole("button", { name: "Unlock", exact: true }).click();
  await appSwitch(page).click();
  await expect(appSwitch(page)).toBeChecked();
  await section(page, "Updates").click();
  await expect(updates(page)).not.toContainText("checks are turned off");
  await expect(desktop(page).getByRole("button", { name: "Update CARE Clinic", exact: true })).toBeVisible();
  expect((await calls(page, "CheckAppUpdate")).length).toBeGreaterThan(0);
});

test("CARE update checks off hide the staged update, check controls and banner", async ({ page }) => {
  await openPanel(page, "scenario=panel-update&checksOff=care");
  await page.evaluate(() => window.careTest.emit("care-update", { backend: "preview-backend-next", frontend: "" }));
  await expect(careBanner(page)).toHaveCount(0);
  await section(page, "Updates").click();
  await expect(updates(page).getByRole("status").filter({ hasText: "CARE software update checks are turned off" })).toBeVisible();
  await expect(software(page)).toContainText("Checks off");
  await expect(software(page)).not.toContainText("Ready to install");
  await expect(software(page).getByRole("button", { name: "Check CARE software updates" })).toHaveCount(0);
  await expect(software(page).getByRole("button", { name: "Install now", exact: true })).toHaveCount(0);
  await expect(desktop(page).getByRole("button", { name: "Check CARE Clinic updates" })).toBeVisible();
  expect(await calls(page, "CheckCareUpdate")).toHaveLength(0);

  await page.evaluate(() => { window.careTest.fixtures.updateChecks = { app: true, care: true }; });
  await section(page, "Overview").click();
  await section(page, "Updates").click();
  await expect(updates(page)).not.toContainText("checks are turned off");
  await expect(software(page)).toContainText("Ready to install");
  await expect(software(page).getByRole("button", { name: "Check CARE software updates" })).toBeVisible();
});

test("both off are named together", async ({ page }) => {
  await openPanel(page, "scenario=panel-current&checksOff=app,care");
  await section(page, "Updates").click();
  await expect(updates(page).getByRole("status").filter({ hasText: "Update checks are turned off" })).toContainText(
    "won't look for new versions of CARE or of this app");
  await expect(software(page)).toContainText("Checks off");
  await expect(desktop(page)).toContainText("Checks off");
});

test("turning CARE checks off clears a banner that was already showing", async ({ page }) => {
  await openPanel(page, "scenario=panel-update");
  await page.evaluate(() => window.careTest.emit("care-update", { backend: "preview-backend-next", frontend: "" }));
  await expect(careBanner(page)).toBeVisible();
  await unlockAdvanced(page);
  await careSwitch(page).click();
  await expect(careSwitch(page)).not.toBeChecked();
  await section(page, "Overview").click();
  await expect(overview(page).getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
  await expect(careBanner(page)).toHaveCount(0);
});

test("after the clinic is removed, update checks are read again and resume", async ({ page }) => {
  await openPanel(page, "scenario=available&screen=panel&checksOff=app,care");
  await unlockAdvanced(page);
  await advanced(page).getByRole("button", { name: "Uninstall…", exact: true }).click();
  const dialog = page.getByRole("alertdialog");
  await page.evaluate(() => { window.careTest.fixtures.finishJobs = false; });
  await dialog.getByLabel("Type DELETE to confirm", { exact: true }).fill("DELETE");
  await dialog.getByRole("button", { name: "Delete everything", exact: true }).click();
  await expect(dialog).toBeHidden();
  const readsBefore = (await calls(page, "GetUpdateChecks")).length;
  const checksBefore = (await calls(page, "CheckAppUpdate")).length;
  await page.evaluate(() => {
    const preview = window.careTest;
    preview.state.role = "";
    preview.state.setup_done = false;
    preview.fixtures.updateChecks = { app: true, care: true };
    preview.emit("uninstalled", true);
    preview.emit("care-done", 0, "uninstall");
  });
  await expect.poll(async () => (await calls(page, "GetUpdateChecks")).length).toBeGreaterThan(readsBefore);
  await expect.poll(async () => (await calls(page, "CheckAppUpdate")).length).toBeGreaterThan(checksBefore);
});

test("a CARE update that is ready to install cannot be closed away", async ({ page }) => {
  await openPanel(page, "scenario=panel-update");
  await page.evaluate(() => window.careTest.emit("care-update", { backend: "preview-backend-next", frontend: "" }));
  await section(page, "Updates").click();
  await expect(software(page).getByRole("button", { name: /Install now/ })).toBeVisible();
  await expect(software(page).getByRole("button", { name: /^Dismiss/ })).toHaveCount(0);
  await expect(updates(page).getByRole("button", { name: "Dismiss: Automatic update checks are on" })).toBeVisible();
});
