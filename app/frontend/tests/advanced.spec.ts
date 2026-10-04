import { mkdir, readFile } from "node:fs/promises";
import { join, relative, resolve } from "node:path";
import { expect, test, type Page } from "@playwright/test";
import type {} from "./fixtures/host";
import { editableEnvLines, mergeEnvChanges, validateExtraSetting, validateSetting } from "../src/screens/panel/advanced-env";
import { GROUPS, isHiddenKey, SETTING_BY_KEY, SETTINGS } from "../src/screens/panel/env-schema";
import { getValue } from "../src/lib/env-file";

const desktopPassword = "ClinicTest123";
const nextPassword = "ClinicChanged456";
const recoveryCode = "0123-4567-89AB-CDEF-0123-4567-89AB-CDEF";
const unavailableSettings = [
  "REACT_DISABLE_PATIENT_LOGIN=true", "USE_SMS=False",
  "SMS_BACKEND=care.utils.sms.backend.console.ConsoleBackend",
  "SNS_ACCESS_KEY=preview-sns-access", "SNS_SECRET_KEY='preview-sns-secret'", "SNS_REGION=ap-south-1",
  "OTP_VALIDITY_MINUTES=10", "OTP_MAX_FAILURES=5", "OTP_LOCKOUT_MINUTES=60", "REACT_APP_RESEND_OTP_TIMEOUT=30",
  "EMAIL_HOST=smtp.example.test", "EMAIL_PORT=587", "EMAIL_USER=clinic@example.test",
  "EMAIL_PASSWORD='preview-mail-secret'", "EMAIL_FROM=clinic@example.test",
  "DEFAULT_FROM_EMAIL=clinic@example.test", "SERVER_EMAIL=server@example.test", "SMTP_HOST=smtp.example.test",
  "ENABLE_OTP_LOGIN=False", "ENABLE_MFA=True", "MFA_ENABLED=True", "TOTP_ISSUER=CARE",
  "REACT_ENABLE_MFA=true", "REACT_MFA_ENABLED=true", "RECAPTCHA_SECRET_KEY='preview-captcha-secret'",
];
const extraSettings = [
  "DISABLE_RATELIMIT=False", "CORAZA_MODE=DetectionOnly",
  "REACT_DEFAULT_COUNTRY=IN", "REACT_DEFAULT_COUNTRY_NAME=India",
  "REACT_PATIENT_REG_MIN_GEO_ORG_LEVELS_REQUIRED=0",
];
const environment = [
  "# Simulated clinic settings", "  ", "DJANGO_SECRET_KEY='test-only-secret'",
  "ADDITIONAL_PLUGS='preview-plugin-config'", "DB_BACKUP_RETENTION_PERIOD=0",
  "JWT_REFRESH_TOKEN_LIFETIME=30",
  ...unavailableSettings, "REACT_APP_TITLE=Sunrise Clinic", "REACT_DEFAULT_ENCOUNTER_TYPE=",
  "REACT_ALLOWED_LOCALES=en,ml", ...extraSettings,
  "",
].join("\r\n");
const backendEnvironment = environment.split("\r\n").filter((line) => !line.startsWith("REACT_")).join("\r\n");
const frontendEnvironment = environment.split("\r\n").filter((line) => line.startsWith("REACT_")).join("\r\n") + "\r\n";
const root = (page: Page) => page.locator(".care-advanced");
const dialog = (page: Page) => page.getByRole("alertdialog");
const count = (page: Page, method: string) => page.evaluate((name) =>
  window.careTest.calls.filter((call) => call.method === name).length, method);

test.use({ trace: "off" });
test.beforeEach(async ({ page }) => {
  await page.routeWebSocket("**", (socket) => socket.close());
});

async function openAdvanced(page: Page) {
  await page.goto("/tests/fixtures/index.html?scenario=panel-running");
  await page.waitForFunction(() => !!window.careTest);
  await page.evaluate(({ backend, frontend, code }) => {
    const fixtures = window.careTest.fixtures;
    fixtures.env = { backend, frontend };
    fixtures.rancherInstalled = true;
    fixtures.canRemoveApp = true;
    fixtures.recoveryCodes[0] = code;
  }, { backend: backendEnvironment, frontend: frontendEnvironment, code: recoveryCode });
  await page.getByRole("button", { name: "Advanced", exact: true }).click();
  await expect(root(page).getByLabel("CARE Clinic admin password", { exact: true })).toBeVisible();
}

async function unlock(page: Page, password = desktopPassword) {
  await root(page).getByLabel("CARE Clinic admin password", { exact: true }).fill(password);
  await root(page).getByRole("button", { name: "Unlock", exact: true }).click();
  await expect(root(page).getByRole("heading", { name: "Clinic settings", exact: true })).toBeVisible();
  await expect(root(page).getByRole("button", { name: /^Staff access/ })).toBeEnabled();
}

async function group(page: Page, name: string) {
  await root(page).locator("[data-advanced-group]").filter({ hasText: name }).click();
  await expect(root(page).getByRole("heading", { name, exact: true })).toBeVisible();
}

async function addCustomSetting(page: Page, key: string, value: string) {
  await root(page).getByRole("button", { name: "Add setting", exact: true }).click();
  await root(page).getByLabel("Setting name", { exact: true }).last().fill(key);
  await root(page).getByLabel(`Value for ${key}`, { exact: true }).fill(value);
}

async function fillNewPassword(page: Page, scope = root(page)) {
  await scope.getByLabel("New CARE Clinic admin password", { exact: true }).fill(nextPassword);
  await scope.getByLabel("Confirm new CARE Clinic admin password", { exact: true }).fill(nextPassword);
}

async function openRemoval(page: Page) {
  await root(page).getByRole("button", { name: "Uninstall…", exact: true }).click();
  await expect(dialog(page)).toBeVisible();
  await expect(dialog(page).getByRole("checkbox")).toHaveCount(4);
}

async function blockAdvanced(page: Page) {
  await page.evaluate(() => window.careTest.progress({ phase: "verifying", done: 1, total: 1 }));
  await expect(root(page)).toHaveAttribute("data-panel-blocked", "true");
  expect(await count(page, "InstallAppUpdate")).toBe(0);
}

async function releaseAdvanced(page: Page) {
  await page.evaluate(() => window.careTest.finishUpdate("test verification interrupted"));
  await expect(root(page)).toHaveAttribute("data-panel-blocked", "false");
}

async function screenshot(page: Page, name: string) {
  if (!process.env.CARE_SCREENSHOTS_DIR) return;
  const folder = relative(process.cwd(), resolve(process.env.CARE_SCREENSHOTS_DIR));
  await mkdir(folder, { recursive: true });
  await page.evaluate(() => document.fonts.ready);
  await page.screenshot({ path: join(folder, `${name}.png`), animations: "disabled" });
}

async function fits(page: Page) {
  expect(await page.evaluate(() => {
    const advanced = document.querySelector(".care-advanced")!;
    const modal = document.querySelector(".advanced-dialog");
    const bounds = modal?.getBoundingClientRect();
    return {
      pageX: document.documentElement.scrollWidth > innerWidth,
      pageY: document.documentElement.scrollHeight > innerHeight,
      contentX: advanced.parentElement!.parentElement!.scrollWidth > advanced.parentElement!.parentElement!.clientWidth + 1,
      modalX: !!modal && modal.scrollWidth > modal.clientWidth + 1,
      modalFits: !bounds || (bounds.left >= 0 && bounds.right <= innerWidth + 1 && bounds.top >= 0 && bounds.bottom <= innerHeight + 1),
    };
  })).toEqual({ pageX: false, pageY: false, contentX: false, modalX: false, modalFits: true });
}

test("environment merging preserves unrelated bytes, quotes, multiline secrets and fresh plugin settings", () => {
  const original = "# keep this\r\n \t\r\nDJANGO_SECRET_KEY='one\r\ntwo'\r\nexport JWT_REFRESH_TOKEN_LIFETIME = 30 # note\r\nADDITIONAL_PLUGS='newer plugin edit'\r\nREACT_APP_TITLE=CARE";
  const merged = mergeEnvChanges(original, [{ key: "JWT_REFRESH_TOKEN_LIFETIME", value: "20" }], "backend");
  expect(merged).toBe(original.replace("export JWT_REFRESH_TOKEN_LIFETIME = 30 # note", "JWT_REFRESH_TOKEN_LIFETIME=20"));
  expect(getValue(editableEnvLines(original), "JWT_REFRESH_TOKEN_LIFETIME")).toBe("30");
  expect(getValue(editableEnvLines(original), "DJANGO_SECRET_KEY")).toBe("one\r\ntwo".replace("\r\n", "\n"));
  expect(mergeEnvChanges(original, [], "backend")).toBe(original);
  expect(mergeEnvChanges("A=one\n", [{ key: "A", value: " leading and trailing " }], "backend")).toBe("A=' leading and trailing '\n");
});

test("environment validators reject unsafe numbers, multiline values and unknown choices", () => {
  const validate = (key: string, value: string) => validateSetting(SETTING_BY_KEY.get(key)!, value);
  expect(validate("JWT_REFRESH_TOKEN_LIFETIME", "4")).toContain("at least 5");
  expect(validate("JWT_REFRESH_TOKEN_LIFETIME", "1.5")).toContain("whole number");
  expect(validate("DB_BACKUP_RETENTION_PERIOD", "0")).toBeNull();
  expect(validate("DB_BACKUP_RETENTION_PERIOD", "")).toContain("Enter a number");
  expect(validate("REACT_APP_TITLE", "Clinic\nNEW_KEY=bad")).toContain("one line");
  expect(validate("REACT_APP_TITLE", "Sunrise Clinic")).toBeNull();
  expect(validate("REACT_DEFAULT_ENCOUNTER_TYPE", "")).toBeNull();
  expect(validate("REACT_DEFAULT_ENCOUNTER_TYPE", "hh")).toBeNull();
  expect(validate("REACT_DEFAULT_ENCOUNTER_TYPE", "Unknown")).toContain("listed");
  expect(validate("REACT_ALLOWED_LOCALES", "")).toContain("at least one");
  expect(validate("REACT_ALLOWED_LOCALES", "unknown")).toContain("unlisted");
});

test("support overrides require a valid default area UUID without restricting other blank values", () => {
  const key = "REACT_PATIENT_REGISTRATION_DEFAULT_GEO_ORG";
  const areaId = "ea3f8598-3d8e-4a8b-b849-06128ae2e9a7";
  for (const value of [
    "", "Kerala", ` ${areaId} `, areaId.replace("-4a8b-", "-9a8b-"), areaId.replace("-b849-", "-7849-"),
  ]) {
    expect(validateExtraSetting(key, value), value).toContain("remove this setting");
  }
  for (const value of [
    areaId, areaId.toUpperCase(), "01933cc4-e57f-785b-955f-2f00e2a241bb",
    "00000000-0000-0000-0000-000000000000", "ffffffff-ffff-ffff-ffff-ffffffffffff",
  ]) {
    expect(validateExtraSetting(key, value), value).toBeNull();
  }
  expect(validateExtraSetting("CUSTOM_OPTION", "")).toBeNull();
  expect(validateExtraSetting("CUSTOM_OPTION", "value\nNEW_KEY=bad")).toContain("one line");
  expect(validateExtraSetting("REACT_PATIENT_REG_MIN_GEO_ORG_LEVELS_REQUIRED", "0")).toBeNull();
});

test("Advanced exposes only ten everyday clinic choices and keeps local extras overridable", () => {
  expect(GROUPS.map((entry) => entry.id)).toEqual([
    "branding", "visits", "billing", "backups", "signin",
  ]);
  expect(SETTINGS.map((setting) => setting.key)).toEqual([
    "REACT_APP_TITLE", "REACT_ALLOWED_LOCALES", "REACT_DEFAULT_ENCOUNTER_TYPE",
    "REACT_ENABLE_MINIMAL_PATIENT_REGISTRATION", "REACT_DEFAULT_PAYMENT_METHOD",
    "REACT_DEFAULT_PAYMENT_TERMS", "REACT_INVENTORY_DEFAULT_TAX_INCLUSIVE",
    "REACT_ENABLE_AUTO_INVOICE_AFTER_DISPENSE", "DB_BACKUP_RETENTION_PERIOD", "JWT_REFRESH_TOKEN_LIFETIME",
  ]);
  expect(SETTINGS.every((setting) => !isHiddenKey(setting.key))).toBe(true);
  for (const entry of unavailableSettings) {
    const key = entry.split("=")[0];
    expect(SETTING_BY_KEY.has(key), key).toBe(false);
    expect(isHiddenKey(key), key).toBe(true);
  }
  for (const key of [
    ...extraSettings.map((entry) => entry.split("=")[0]),
    "TIME_ZONE", "CUSTOM_SECRET", "REACT_CARE_ENABLE_DASHBOARD", "REACT_PATIENT_REGISTRATION_DEFAULT_GEO_ORG",
    "REACT_MAIN_LOGO", "REACT_AUTO_REFRESH_INTERVAL", "REACT_ALLOWED_ENCOUNTER_CLASSES",
  ]) {
    expect(SETTING_BY_KEY.has(key), key).toBe(false);
    expect(isHiddenKey(key), key).toBe(false);
  }
});

test("new clinic templates include visit and address defaults without an invalid blank area ID", async () => {
  const contents = await readFile(new URL("../../../deployments/frontend.env", import.meta.url), "utf8");
  const lines = editableEnvLines(contents);
  expect(getValue(lines, "REACT_DEFAULT_ENCOUNTER_TYPE")).toBe("");
  expect(getValue(lines, "REACT_PATIENT_REG_MIN_GEO_ORG_LEVELS_REQUIRED")).toBe("0");
  expect(getValue(lines, "REACT_PATIENT_REGISTRATION_DEFAULT_GEO_ORG")).toBeUndefined();
  expect(contents).toContain("# REACT_PATIENT_REGISTRATION_DEFAULT_GEO_ORG=");
});

test("email, SMS, patient sign-in and MFA settings cannot reappear in support-only extras", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await expect(root(page).locator("[data-advanced-group]")).toHaveCount(6);
  await expect(root(page).getByRole("button", { name: /^Patient sign-in/ })).toHaveCount(0);
  await expect(root(page).getByRole("button", { name: /^Outgoing email/ })).toHaveCount(0);
  await group(page, "Extra settings (for support)");
  const existingNames = root(page).getByLabel("Setting name", { exact: true });
  await expect(existingNames).toHaveCount(extraSettings.length);
  for (const [index, entry] of extraSettings.entries()) {
    await expect(existingNames.nth(index)).toHaveValue(entry.split("=")[0]);
  }
  await root(page).getByRole("button", { name: "Add setting", exact: true }).click();
  const name = root(page).getByLabel("Setting name", { exact: true }).last();
  const save = root(page).getByRole("button", { name: "Save changes", exact: true });
  for (const entry of unavailableSettings) {
    await name.fill(entry.split("=")[0]);
    await expect(root(page)).toContainText("This setting is protected by CARE Clinic.");
    await expect(save).toBeDisabled();
  }
  await save.dispatchEvent("click");
  expect(await count(page, "WriteEnv")).toBe(0);
  expect(await count(page, "ClinicAction")).toBe(0);
});

for (const size of [{ width: 1100, height: 700 }, { width: 720, height: 560 }]) {
  test(`Advanced gate, groups, recovery and removal fit ${size.width}x${size.height}`, async ({ page }) => {
    await page.setViewportSize(size);
    await openAdvanced(page);
    await expect(root(page).getByLabel("CARE Clinic admin password", { exact: true })).toBeFocused();
    await fits(page);
    await screenshot(page, `advanced-gate-${size.width}x${size.height}`);
    await root(page).getByRole("button", { name: "Forgot CARE Clinic password?" }).click();
    await expect(root(page).getByLabel("Unused recovery code", { exact: true })).toBeFocused();
    await fits(page);
    await screenshot(page, `advanced-recovery-form-${size.width}x${size.height}`);
    await root(page).getByRole("button", { name: "Cancel", exact: true }).click();
    await unlock(page);
    await expect(root(page)).toContainText("Keep backups forever");
    await expect(root(page)).toContainText("Sunrise Clinic");
    await screenshot(page, `advanced-overview-${size.width}x${size.height}`);
    for (const entry of GROUPS) {
      await group(page, entry.title);
      await expect(root(page).locator(".advanced-setting")).toHaveCount(entry.settings.length);
      await fits(page);
      await root(page).getByRole("button", { name: "Back to settings", exact: true }).click();
    }
    await group(page, "Staff access");
    await fits(page);
    await screenshot(page, `advanced-signin-${size.width}x${size.height}`);
    await root(page).getByRole("button", { name: "Back to settings", exact: true }).click();
    await root(page).getByRole("button", { name: "Change password", exact: true }).click();
    await expect(dialog(page)).toBeVisible();
    await fits(page);
    await screenshot(page, `advanced-password-${size.width}x${size.height}`);
    await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
    await openRemoval(page);
    await expect(dialog(page).getByRole("button", { name: "Cancel", exact: true })).toBeFocused();
    await fits(page);
    await screenshot(page, `advanced-removal-${size.width}x${size.height}`);
    await page.keyboard.press("Escape");
    await expect(dialog(page)).toBeHidden();
  });
}

test("Advanced navigation never starts writes, rebuilds, recovery resets or removals", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await group(page, "Staff access");
  await root(page).getByRole("button", { name: "Back to settings" }).click();
  await openRemoval(page);
  await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
  for (const method of ["WriteEnv", "ClinicAction", "RunUninstall", "ResetAdminPassword", "ChangeAdminPassword", "SaveAdminRecoveryCodes"]) {
    expect(await count(page, method)).toBe(0);
  }
});

test("Desktop gate rejects web passwords, redacts errors and prevents duplicate verification", async ({ page }) => {
  await openAdvanced(page);
  await root(page).getByLabel("CARE Clinic admin password", { exact: true }).fill("WebPassword987");
  await root(page).getByRole("button", { name: "Unlock", exact: true }).click();
  await expect(root(page)).toContainText("That's not the CARE Clinic admin password");
  await expect(root(page).getByLabel("CARE Clinic admin password", { exact: true })).toHaveValue("");
  await page.evaluate(() => window.careTest.hold("VerifyAdminPassword"));
  await root(page).getByLabel("CARE Clinic admin password", { exact: true }).fill(desktopPassword);
  await root(page).getByRole("button", { name: "Unlock", exact: true }).click();
  await page.keyboard.press("Enter");
  expect(await count(page, "VerifyAdminPassword")).toBe(2);
  await page.evaluate(() => window.careTest.release("VerifyAdminPassword"));
  await expect(root(page).getByRole("heading", { name: "Clinic settings" })).toBeVisible();
  await root(page).getByRole("button", { name: "Lock Advanced settings" }).click();
  await page.evaluate(() => window.careTest.failNext("VerifyAdminPassword", "read /private/clinic/admin.hash: secret diagnostic"));
  await root(page).getByLabel("CARE Clinic admin password", { exact: true }).fill(desktopPassword);
  await root(page).getByRole("button", { name: "Unlock", exact: true }).click();
  await expect(root(page)).toContainText("The password couldn't be checked");
  await expect(root(page)).not.toContainText("/private/clinic");
});

test("leaving Advanced clears sensitive inputs and requires a new unlock", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await group(page, "Extra settings (for support)");
  await addCustomSetting(page, "CUSTOM_SECRET", "UnsavedPreview789");
  await root(page).getByRole("button", { name: "Show value for custom_secret", exact: true }).click();
  await expect(root(page).getByLabel("Value for CUSTOM_SECRET", { exact: true })).toHaveAttribute("type", "text");
  await page.getByRole("button", { name: "Overview", exact: true }).click();
  await expect(page.getByLabel("Value for CUSTOM_SECRET", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Advanced", exact: true }).click();
  await expect(root(page).getByLabel("CARE Clinic admin password", { exact: true })).toHaveValue("");
  await unlock(page);
  await group(page, "Extra settings (for support)");
  await expect(root(page).getByLabel("Value for CUSTOM_SECRET", { exact: true })).toHaveCount(0);
  expect(await count(page, "WriteEnv")).toBe(0);
});

test("group validation, discard and back confirmation never save implicitly", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await group(page, "Staff access");
  const minutes = root(page).getByRole("spinbutton", { name: "Sign out inactive staff after" });
  await minutes.fill("4");
  await expect(root(page)).toContainText("Must be at least 5");
  await expect(root(page).getByRole("button", { name: "Save changes" })).toBeDisabled();
  await minutes.fill("20");
  await root(page).getByRole("button", { name: "Back to settings" }).click();
  await expect(dialog(page)).toContainText("Discard unsaved settings?");
  await dialog(page).getByRole("button", { name: "Keep editing" }).click();
  await expect(minutes).toHaveValue("20");
  await root(page).getByRole("button", { name: "Discard", exact: true }).click();
  await expect(minutes).toHaveValue("30");
  expect(await count(page, "WriteEnv")).toBe(0);
});

test("saving merges fresh settings once and waits for the accepted restart", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await group(page, "Staff access");
  await root(page).getByRole("spinbutton", { name: "Sign out inactive staff after" }).fill("20");
  await page.evaluate((fresh) => {
    window.careTest.fixtures.env.backend = fresh;
    window.careTest.fixtures.finishJobs = false;
    window.careTest.hold("WriteEnv");
  }, backendEnvironment.replace("preview-plugin-config", "newer-plugin-config"));
  await root(page).getByRole("button", { name: "Save changes" }).click();
  await expect(root(page).getByRole("button", { name: "Save changes" })).toBeDisabled();
  await page.keyboard.press("Enter");
  await expect.poll(() => count(page, "WriteEnv")).toBe(1);
  await page.evaluate(() => window.careTest.release("WriteEnv"));
  await expect.poll(() => count(page, "ClinicAction")).toBe(1);
  const result = await page.evaluate(() => {
    const write = window.careTest.calls.find((call) => call.method === "WriteEnv")!;
    return { file: write.args[0], changed: String(write.args[1]).includes("JWT_REFRESH_TOKEN_LIFETIME=20"),
      pluginKept: String(write.args[1]).includes("newer-plugin-config"),
      secretsKept: String(write.args[1]).includes("DJANGO_SECRET_KEY='test-only-secret'"),
      CRLFKept: String(write.args[1]).includes("\r\n  \r\n"), passwordMatches: write.args[2] === window.careTest.fixtures.adminPassword };
  });
  expect(result).toEqual({ file: "backend", changed: true, pluginKept: true, secretsKept: true, CRLFKept: true, passwordMatches: true });
  expect(await page.evaluate(() => window.careTest.fixtures.env)).toEqual({
    backend: backendEnvironment.replace("preview-plugin-config", "newer-plugin-config")
      .replace("JWT_REFRESH_TOKEN_LIFETIME=30", "JWT_REFRESH_TOKEN_LIFETIME=20"),
    frontend: frontendEnvironment,
  });
  await expect(root(page)).toContainText("Applying settings");
  await page.evaluate(() => window.careTest.finishJob("start"));
  await expect(root(page)).toContainText("Settings applied.");
});

test("a saved setting isn't presented as applied when restart acceptance fails", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await group(page, "Staff access");
  await root(page).getByRole("spinbutton", { name: "Sign out inactive staff after" }).fill("20");
  await page.evaluate(() => window.careTest.failNext("ClinicAction", "something else is still running"));
  await root(page).getByRole("button", { name: "Save changes" }).click();
  await expect(root(page)).toContainText("Settings were saved, but applying them didn't start");
  await expect(root(page).getByRole("button", { name: "Apply saved changes" })).toBeEnabled();
  await expect(root(page)).not.toContainText("Settings applied.");
  await root(page).getByRole("button", { name: "Lock Advanced settings" }).click();
  await unlock(page);
  await expect(root(page).getByRole("button", { name: "Apply saved changes" })).toBeEnabled();
  await root(page).getByRole("button", { name: "Apply saved changes" }).click();
  await expect(root(page)).toContainText("Settings applied.");
});

test("the usual visit has no preset until an admin chooses one, then applies with a frontend rebuild", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await group(page, "Patients and visits");
  const visit = root(page).getByRole("combobox", { name: "Usual visit type", exact: true });
  await expect(visit).toHaveText("No preference");
  await visit.click();
  await page.getByRole("option", { name: "Home visit", exact: true }).click();
  await root(page).getByRole("button", { name: "Save changes", exact: true }).click();
  await expect(root(page)).toContainText("Settings applied.");
  expect(await page.evaluate(() => window.careTest.fixtures.env)).toEqual({
    backend: backendEnvironment,
    frontend: frontendEnvironment.replace("REACT_DEFAULT_ENCOUNTER_TYPE=", "REACT_DEFAULT_ENCOUNTER_TYPE=hh"),
  });
  expect(await page.evaluate(() => window.careTest.calls.filter((call) => call.method === "ClinicAction").map((call) => call.args[0])))
    .toEqual(["rebuild-frontend"]);
});

test("usual visit choices respect the enabled visit types set by support", async ({ page }) => {
  await openAdvanced(page);
  await page.evaluate(() => { window.careTest.fixtures.env.frontend += "REACT_ALLOWED_ENCOUNTER_CLASSES=amb\r\n"; });
  await unlock(page);
  await group(page, "Patients and visits");
  const visit = root(page).getByRole("combobox", { name: "Usual visit type", exact: true });
  await visit.click();
  await page.getByRole("option", { name: "Home visit", exact: true }).click();
  await expect(root(page)).toContainText("This visit type is not enabled for your clinic.");
  await expect(root(page).getByRole("button", { name: "Save changes", exact: true })).toBeDisabled();
  expect(await count(page, "WriteEnv")).toBe(0);
  await visit.click();
  await page.getByRole("option", { name: "Outpatient visit", exact: true }).click();
  await root(page).getByRole("button", { name: "Save changes", exact: true }).click();
  await expect(root(page)).toContainText("Settings applied.");
  const contents = await page.evaluate(() => window.careTest.fixtures.env.frontend);
  expect(getValue(editableEnvLines(contents), "REACT_DEFAULT_ENCOUNTER_TYPE")).toBe("amb");
  expect(getValue(editableEnvLines(contents), "REACT_ALLOWED_ENCOUNTER_CLASSES")).toBe("amb");
});

test("support cannot save blank or malformed default area IDs", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await group(page, "Extra settings (for support)");
  const key = "REACT_PATIENT_REGISTRATION_DEFAULT_GEO_ORG";
  await addCustomSetting(page, key, "");
  const area = root(page).getByLabel(`Value for ${key}`, { exact: true });
  const save = root(page).getByRole("button", { name: "Save changes", exact: true });
  for (const value of ["", "Kerala", "ea3f8598-3d8e-9a8b-b849-06128ae2e9a7"]) {
    await area.fill(value);
    await expect(root(page)).toContainText("Enter a valid geographic area ID");
    await expect(save).toBeDisabled();
  }
  await save.dispatchEvent("click");
  expect(await count(page, "WriteEnv")).toBe(0);
  expect(await count(page, "ClinicAction")).toBe(0);
});

test("support can override address defaults, preserve zero and remove the default area without resetting other settings", async ({ page }) => {
  await openAdvanced(page);
  await page.evaluate(() => {
    window.careTest.fixtures.env.frontend = window.careTest.fixtures.env.frontend
      .replace("REACT_PATIENT_REG_MIN_GEO_ORG_LEVELS_REQUIRED=0", "REACT_PATIENT_REG_MIN_GEO_ORG_LEVELS_REQUIRED=3");
  });
  await unlock(page);
  await group(page, "Extra settings (for support)");
  const levels = root(page).getByLabel("Value for REACT_PATIENT_REG_MIN_GEO_ORG_LEVELS_REQUIRED", { exact: true });
  const area = root(page).getByLabel("Value for REACT_PATIENT_REGISTRATION_DEFAULT_GEO_ORG", { exact: true });
  await expect(levels).toHaveValue("3");
  await expect(area).toHaveCount(0);
  await levels.fill("0");
  const areaId = "ea3f8598-3d8e-4a8b-b849-06128ae2e9a7";
  await addCustomSetting(page, "REACT_PATIENT_REGISTRATION_DEFAULT_GEO_ORG", areaId);
  await addCustomSetting(page, "REACT_AUTO_REFRESH_INTERVAL", "60");
  await root(page).getByRole("button", { name: "Save changes", exact: true }).click();
  await expect(root(page)).toContainText("Settings applied.");
  const saved = await page.evaluate(() => window.careTest.fixtures.env);
  expect(saved.backend).toBe(backendEnvironment);
  const lines = editableEnvLines(saved.frontend);
  expect(getValue(lines, "REACT_PATIENT_REG_MIN_GEO_ORG_LEVELS_REQUIRED")).toBe("0");
  expect(getValue(lines, "REACT_PATIENT_REGISTRATION_DEFAULT_GEO_ORG")).toBe(areaId);
  expect(getValue(lines, "REACT_AUTO_REFRESH_INTERVAL")).toBe("60");
  expect(getValue(lines, "REACT_DEFAULT_ENCOUNTER_TYPE")).toBe("");
  await expect(levels).toHaveValue("0");
  await area.fill("");
  await expect(root(page).getByRole("button", { name: "Save changes", exact: true })).toBeDisabled();
  await root(page).getByRole("button", { name: "Remove REACT_PATIENT_REGISTRATION_DEFAULT_GEO_ORG", exact: true }).click();
  await root(page).getByRole("button", { name: "Save changes", exact: true }).click();
  await expect.poll(() => count(page, "WriteEnv")).toBe(2);
  await expect(root(page)).toContainText("Settings applied.");
  const cleared = editableEnvLines(await page.evaluate(() => window.careTest.fixtures.env.frontend));
  expect(getValue(cleared, "REACT_PATIENT_REGISTRATION_DEFAULT_GEO_ORG")).toBeUndefined();
  expect(getValue(cleared, "REACT_PATIENT_REG_MIN_GEO_ORG_LEVELS_REQUIRED")).toBe("0");
  expect(getValue(cleared, "REACT_AUTO_REFRESH_INTERVAL")).toBe("60");
});

test("protected and everyday keys cannot be duplicated in support-only extras", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await group(page, "Extra settings (for support)");
  await root(page).getByRole("button", { name: "Add setting" }).click();
  const name = root(page).getByLabel("Setting name", { exact: true }).last();
  await name.fill("DJANGO_SECRET_KEY");
  await expect(root(page)).toContainText("Generated and protected by CARE Clinic");
  await expect(root(page).getByRole("button", { name: "Save changes" })).toBeDisabled();
  await name.fill("ADDITIONAL_PLUGS");
  await expect(root(page)).toContainText("Use the Plugins tab instead");
  await name.fill("JWT_REFRESH_TOKEN_LIFETIME");
  await expect(root(page)).toContainText("in its settings group instead");
  await name.fill("1INVALID");
  await expect(root(page)).toContainText("Start with a letter");
  expect(await count(page, "WriteEnv")).toBe(0);
});

test("password reset validates the pair, handles used codes, and clears all sensitive fields", async ({ page }) => {
  await openAdvanced(page);
  await root(page).getByRole("button", { name: "Forgot CARE Clinic password?" }).click();
  await expect(root(page)).toContainText("Password-only backup key downloads will require re-enrollment");
  await root(page).getByLabel("Unused recovery code", { exact: true }).fill(recoveryCode);
  await fillNewPassword(page);
  await root(page).getByLabel("Confirm new CARE Clinic admin password", { exact: true }).fill("Different789");
  await expect(root(page).getByRole("button", { name: "Reset CARE Clinic password", exact: true })).toBeDisabled();
  await root(page).getByLabel("Confirm new CARE Clinic admin password", { exact: true }).fill(nextPassword);
  await page.evaluate(() => {
    window.careTest.failNext("ResetAdminPassword", "that recovery code is invalid or already used; use an unused code from the latest set");
  });
  await root(page).getByRole("button", { name: "Reset CARE Clinic password", exact: true }).click();
  await expect(root(page)).toContainText("That recovery code didn't work");
  for (const label of ["Unused recovery code", "New CARE Clinic admin password", "Confirm new CARE Clinic admin password"]) {
    await expect(root(page).getByLabel(label, { exact: true })).toHaveValue("");
  }
  await root(page).getByLabel("Unused recovery code", { exact: true }).fill(recoveryCode);
  await fillNewPassword(page);
  await page.evaluate(() => window.careTest.hold("ResetAdminPassword"));
  await root(page).getByRole("button", { name: "Reset CARE Clinic password", exact: true }).click();
  await page.keyboard.press("Enter");
  expect(await count(page, "ResetAdminPassword")).toBe(2);
  await page.evaluate(() => window.careTest.release("ResetAdminPassword"));
  await expect(root(page).getByRole("heading", { name: "Clinic settings" })).toBeVisible();
  await expect(page.getByLabel("Unused recovery code", { exact: true })).toHaveCount(0);
  await expect(page.getByText(/Mark that recovery code used/)).toBeVisible();
  expect(await page.evaluate(() => window.careTest.fixtures.recoveryCodes.filter(Boolean).length)).toBe(5);
  expect(await page.evaluate(() => window.careTest.fixtures.recovery.backup_key_needs_enrollment)).toBe(true);
  await root(page).getByRole("button", { name: "Lock Advanced settings" }).click();
  await root(page).getByRole("button", { name: "Forgot CARE Clinic password?" }).click();
  await root(page).getByLabel("Unused recovery code", { exact: true }).fill(recoveryCode);
  await fillNewPassword(page);
  await root(page).getByRole("button", { name: "Reset CARE Clinic password", exact: true }).click();
  await expect(root(page)).toContainText("That recovery code didn't work");
  expect(await page.evaluate(() => window.careTest.fixtures.recoveryCodes.filter(Boolean).length)).toBe(5);
});

test("recovery rate limits are friendly and disable retry until the native wait expires", async ({ page }) => {
  await openAdvanced(page);
  await root(page).getByRole("button", { name: "Forgot CARE Clinic password?" }).click();
  await root(page).getByLabel("Unused recovery code", { exact: true }).fill(recoveryCode);
  await fillNewPassword(page);
  await page.evaluate(() => window.careTest.failNext("ResetAdminPassword", "too many recovery attempts; try again in 60 seconds"));
  await root(page).getByRole("button", { name: "Reset CARE Clinic password", exact: true }).click();
  await expect(root(page)).toContainText("Please wait before trying another code");
  await expect(root(page)).toContainText(/Try again in \d+ seconds/);
  await root(page).getByLabel("Unused recovery code", { exact: true }).fill(recoveryCode);
  await fillNewPassword(page);
  await expect(root(page).getByRole("button", { name: "Reset CARE Clinic password", exact: true })).toBeDisabled();
  await root(page).getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(root(page).getByLabel("CARE Clinic admin password", { exact: true })).toHaveValue("");
});

test("changing the CARE Clinic password doesn't reset the web login or replace recovery codes", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  const originalCodes = await page.evaluate(() => window.careTest.fixtures.recoveryCodes);
  await root(page).getByRole("button", { name: "Change password", exact: true }).click();
  await fillNewPassword(page, dialog(page));
  await dialog(page).getByRole("button", { name: "Change CARE Clinic password", exact: true }).click();
  await expect(dialog(page)).toBeHidden();
  await expect(root(page)).toContainText("Your CARE web login and unused recovery codes are unchanged");
  expect(await count(page, "ChangeAdminPassword")).toBe(1);
  expect(await count(page, "ResetAdminPassword")).toBe(0);
  expect(await count(page, "SaveAdminRecoveryCodes")).toBe(0);
  expect(await page.evaluate(() => window.careTest.fixtures.recoveryCodes)).toEqual(originalCodes);
  await expect(root(page).getByRole("button", { name: "Change password", exact: true })).toBeEnabled();
  await page.evaluate(() => {
    window.careTest.fixtures.recoveryFile = "";
    window.careTest.fixtures.recovery.backup_path = "";
  });
  await page.getByRole("button", { name: "Backups", exact: true }).click();
  await page.getByRole("button", { name: "Re-download backup key", exact: true }).click();
  await dialog(page).getByLabel("CARE Clinic admin password", { exact: true }).fill(nextPassword);
  await dialog(page).getByRole("button", { name: "Choose where to save", exact: true }).click();
  await expect(dialog(page)).toBeHidden();
  expect(await count(page, "ExportBackupRecovery")).toBe(1);
});

test("replacement-code cancellation keeps the previous codes valid", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await root(page).getByRole("button", { name: "New recovery codes" }).click();
  await expect(dialog(page)).toContainText("invalidates every previous code");
  await page.evaluate(() => window.careTest.respond("SaveAdminRecoveryCodes", false));
  await dialog(page).getByRole("button", { name: "Choose where to save" }).click();
  await expect(dialog(page)).toContainText("No new codes were saved");
  await expect(dialog(page)).toContainText("existing unused codes still work");
  expect(await page.evaluate(() => window.careTest.fixtures.recoveryGeneration)).toBe(1);
  await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(root(page).getByRole("button", { name: "New recovery codes" })).toBeFocused();
});

test("replacement-code success invalidates the previous set and reports the saved location", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  const originalCodes = await page.evaluate(() => window.careTest.fixtures.recoveryCodes);
  await root(page).getByRole("button", { name: "New recovery codes" }).click();
  await page.evaluate(() => {
    window.careTest.hold("SaveAdminRecoveryCodes");
  });
  await dialog(page).getByRole("button", { name: "Choose where to save" }).click();
  await expect(dialog(page).getByRole("button", { name: "Cancel", exact: true })).toBeDisabled();
  await page.keyboard.press("Enter");
  expect(await count(page, "SaveAdminRecoveryCodes")).toBe(1);
  await page.evaluate(() => window.careTest.release("SaveAdminRecoveryCodes"));
  await expect(dialog(page)).toBeHidden();
  await expect(root(page)).toContainText("Every previous code is now invalid");
  await expect(root(page)).toContainText("/test-fixtures/recovery/CARE-clinic-admin-codes.txt");
  const nextCodes = await page.evaluate(() => window.careTest.fixtures.recoveryCodes);
  expect(nextCodes).toHaveLength(6);
  expect(nextCodes.every((code) => !originalCodes.includes(code))).toBe(true);
});

for (const [failure, expected] of [
  ["keep recovery materials outside CARE's settings, installation, logs and backup folder", "Choose a separate place for the codes"],
  ["could not save the recovery file; choose a new filename: file exists", "Choose a new filename"],
  ["could not activate the new recovery codes; keep the previous sheet and retry: private error", "The new codes weren't activated"],
] as const) {
  test(`replacement recovery codes handle ${expected.toLowerCase()}`, async ({ page }) => {
    await openAdvanced(page);
    await unlock(page);
    await root(page).getByRole("button", { name: "New recovery codes" }).click();
    await page.evaluate((error) => window.careTest.failNext("SaveAdminRecoveryCodes", error), failure);
    await dialog(page).getByRole("button", { name: "Choose where to save" }).click();
    await expect(dialog(page)).toContainText(expected);
    await expect(dialog(page)).not.toContainText("private error");
    await expect(dialog(page).getByRole("button", { name: "Choose where to save" })).toBeEnabled();
  });
}

test("removal is typed, all extras default off, cancellation clears choices, and focus stays in the dialog", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await openRemoval(page);
  const remove = dialog(page).getByRole("button", { name: "Delete everything", exact: true });
  await expect(remove).toBeDisabled();
  for (const checkbox of await dialog(page).getByRole("checkbox").all()) await expect(checkbox).not.toBeChecked();
  await dialog(page).getByLabel("Type DELETE to confirm", { exact: true }).fill("delete");
  await expect(remove).toBeDisabled();
  await dialog(page).getByLabel("Type DELETE to confirm", { exact: true }).fill("DELETE");
  await expect(remove).toBeEnabled();
  await dialog(page).getByRole("checkbox", { name: /Also delete the backups/ }).check();
  await page.keyboard.press("Tab");
  expect(await page.evaluate(() => !!document.activeElement?.closest('[role="alertdialog"]'))).toBe(true);
  await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(root(page).getByRole("button", { name: "Uninstall…", exact: true })).toBeFocused();
  await openRemoval(page);
  await expect(dialog(page).getByLabel("Type DELETE to confirm", { exact: true })).toHaveValue("");
  for (const checkbox of await dialog(page).getByRole("checkbox").all()) await expect(checkbox).not.toBeChecked();
  expect(await count(page, "RunUninstall")).toBe(0);
});

test("removal submits exact opt-ins once and remains busy until native completion", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await openRemoval(page);
  await page.evaluate(() => {
    window.careTest.fixtures.finishJobs = false;
    window.careTest.hold("RunUninstall");
  });
  await dialog(page).getByRole("checkbox", { name: /Also remove downloaded images/ }).check();
  await dialog(page).getByRole("checkbox", { name: /Also remove the CARE Clinic app/ }).check();
  await dialog(page).getByLabel("Type DELETE to confirm", { exact: true }).fill("DELETE");
  await dialog(page).getByRole("button", { name: "Delete everything", exact: true }).click();
  await page.keyboard.press("Enter");
  expect(await count(page, "RunUninstall")).toBe(1);
  expect(await page.evaluate(() => {
    const call = window.careTest.calls.find((entry) => entry.method === "RunUninstall")!;
    return { images: call.args[0], backups: call.args[1], rancher: call.args[2],
      passwordMatches: call.args[3] === window.careTest.fixtures.adminPassword };
  })).toEqual({ images: true, backups: false, rancher: false, passwordMatches: true });
  await page.evaluate(() => window.careTest.release("RunUninstall"));
  await expect(dialog(page)).toBeHidden();
  await expect(root(page).getByRole("button", { name: "Uninstall…", exact: true })).toBeDisabled();
  expect(await count(page, "RemoveApp")).toBe(0);
  await page.evaluate(() => window.careTest.finishJob("uninstall", "cleanup failed: native detail"));
  await expect(page.getByRole("alert").filter({ hasText: "Removal didn't finish" }).first()).toBeVisible();
  expect(await count(page, "RemoveApp")).toBe(0);
});

test("unavailable removal options fail closed and an immediate rejection keeps the dialog open", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await page.evaluate(() => window.careTest.failNext("RancherDesktopInstalled", "private native lookup failure"));
  await root(page).getByRole("button", { name: "Uninstall…", exact: true }).click();
  await expect(dialog(page)).toContainText("Removal options couldn't be checked");
  await dialog(page).getByLabel("Type DELETE to confirm", { exact: true }).fill("DELETE");
  await expect(dialog(page).getByRole("button", { name: "Delete everything", exact: true })).toBeDisabled();
  await dialog(page).getByRole("button", { name: "Check removal options again" }).click();
  await expect(dialog(page).getByRole("button", { name: "Delete everything", exact: true })).toBeEnabled();
  await page.evaluate(() => window.careTest.failNext("RunUninstall", "something else is still running"));
  await dialog(page).getByRole("button", { name: "Delete everything", exact: true }).click();
  await expect(dialog(page)).toContainText("Removal didn't start");
  await expect(dialog(page).getByLabel("Type DELETE to confirm", { exact: true })).toHaveValue("");
});

test("rebuild requires its own decision and cancellation does not start it", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await root(page).getByRole("button", { name: "Rebuild", exact: true }).click();
  await expect(dialog(page)).toContainText("Patient data and backups are kept");
  await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
  expect(await count(page, "ClinicAction")).toBe(0);
  await root(page).getByRole("button", { name: "Rebuild", exact: true }).click();
  await dialog(page).getByRole("button", { name: "Rebuild CARE", exact: true }).click();
  await expect(dialog(page)).toBeHidden();
  expect(await count(page, "ClinicAction")).toBe(1);
});

test("unreadable settings never show guessed values or enable saving", async ({ page }) => {
  await openAdvanced(page);
  await page.evaluate(() => window.careTest.failNext("ReadEnv", "read backend.env: private setting detail"));
  await root(page).getByLabel("CARE Clinic admin password", { exact: true }).fill(desktopPassword);
  await root(page).getByRole("button", { name: "Unlock", exact: true }).click();
  await expect(root(page)).toContainText("The clinic settings couldn't be read");
  await expect(root(page)).not.toContainText("Keep backups forever");
  await expect(root(page)).not.toContainText("private setting detail");
  await expect(root(page).getByRole("button", { name: /^Staff access/ })).toBeDisabled();
  await root(page).getByRole("button", { name: "Try reading settings again" }).click();
  await expect(root(page)).toContainText("Keep backups forever");
  expect(await count(page, "WriteEnv")).toBe(0);
});

test("partial saves keep the remaining edits and retry only the unwritten file before applying", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await group(page, "Extra settings (for support)");
  await addCustomSetting(page, "TIME_ZONE", "Asia/Kolkata");
  await addCustomSetting(page, "REACT_CARE_ENABLE_DASHBOARD", "false");
  await page.evaluate(() => {
    const original = Object.getOwnPropertyDescriptor(window.go.main.App, "WriteEnv")!.value as Window["go"]["main"]["App"]["WriteEnv"];
    let written = 0;
    window.go.main.App.WriteEnv = async (...args) => {
      if (++written === 2) throw new Error("write frontend.env: permission denied");
      return original(...args);
    };
  });
  await root(page).getByRole("button", { name: "Save changes" }).click();
  await expect(root(page)).toContainText("Some settings were saved");
  expect(await count(page, "ClinicAction")).toBe(0);
  expect(await page.evaluate(() => ({
    backendChanged: window.careTest.fixtures.env.backend.includes("TIME_ZONE=Asia/Kolkata"),
    frontend: window.careTest.fixtures.env.frontend,
  }))).toEqual({ backendChanged: true, frontend: frontendEnvironment });
  await root(page).getByRole("button", { name: "Save changes" }).click();
  await expect(root(page)).toContainText("Settings applied.");
  expect(await page.evaluate(() => window.careTest.calls.filter((call) => call.method === "WriteEnv").map((call) => call.args[0])))
    .toEqual(["backend", "frontend", "frontend"]);
  expect(await page.evaluate(() => window.careTest.fixtures.env.frontend)).toContain("REACT_CARE_ENABLE_DASHBOARD=false");
  expect(await count(page, "ClinicAction")).toBe(1);
});

test("an unfinished restore disables environment writes, rebuilds and removal", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await page.evaluate(() => {
    window.careTest.state.restore_pending = true;
    window.careTest.emit("care-done", 0, "status");
  });
  await expect(root(page).getByRole("button", { name: "Rebuild", exact: true })).toBeDisabled();
  await expect(root(page).getByRole("button", { name: "Uninstall…", exact: true })).toBeDisabled();
  await group(page, "Staff access");
  await expect(root(page)).toContainText("Finish the earlier restore first");
  await expect(root(page).getByRole("spinbutton", { name: "Sign out inactive staff after" })).toBeDisabled();
  await expect(root(page).getByRole("button", { name: "Save changes" })).toBeDisabled();
  expect(await count(page, "WriteEnv")).toBe(0);
  expect(await count(page, "RunUninstall")).toBe(0);
  expect(await count(page, "ClinicAction")).toBe(0);
});

test("Advanced unlock expires and removes sensitive drafts without starting an operation", async ({ page }) => {
  await page.clock.install();
  await openAdvanced(page);
  await unlock(page);
  await group(page, "Extra settings (for support)");
  await addCustomSetting(page, "CUSTOM_SECRET", "UnsavedPreview789");
  await page.clock.fastForward(14 * 60 * 1000 + 59 * 1000);
  await expect(root(page)).not.toContainText("Advanced has locked");
  await expect(root(page).getByLabel("Value for CUSTOM_SECRET", { exact: true })).toHaveValue("UnsavedPreview789");
  await page.clock.fastForward(1000);
  await expect(root(page)).toContainText("Advanced has locked");
  await expect(root(page).getByLabel("CARE Clinic admin password", { exact: true })).toHaveValue("");
  await expect(page.getByLabel("Value for CUSTOM_SECRET", { exact: true })).toHaveCount(0);
  expect(await count(page, "WriteEnv")).toBe(0);
  expect(await count(page, "ClinicAction")).toBe(0);
});

test("Windows installed-clinic removal requires Desktop authentication and a separate confirmation", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?screen=remove&platform=windows");
  await page.getByLabel("CARE Clinic admin password", { exact: true }).fill(desktopPassword);
  await page.getByRole("button", { name: "Unlock", exact: true }).click();
  await page.getByRole("button", { name: "Uninstall…", exact: true }).click();
  await expect(dialog(page).getByRole("button", { name: "Delete everything", exact: true })).toBeDisabled();
  await dialog(page).getByLabel("Type DELETE to confirm", { exact: true }).fill("DELETE");
  await expect(dialog(page).getByRole("button", { name: "Delete everything", exact: true })).toBeEnabled();
  await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
  expect(await count(page, "RunUninstall")).toBe(0);
  expect(await count(page, "ExitUninstall")).toBe(0);
  expect(await count(page, "RemoveApp")).toBe(0);
});

test("Windows unfinished-setup removal needs typed confirmation and cancellation is safe", async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(window, "careTest", {
      configurable: true,
      set(preview: Window["careTest"]) {
        Object.defineProperty(window, "careTest", { value: preview, configurable: true });
        preview.state.setup_done = false;
      },
    });
  });
  await page.goto("/tests/fixtures/index.html?screen=remove&platform=windows");
  await page.getByRole("button", { name: "Remove unfinished setup…" }).click();
  await expect(dialog(page).getByRole("button", { name: "Remove setup", exact: true })).toBeDisabled();
  await dialog(page).getByLabel("Type DELETE to confirm", { exact: true }).fill("DELETE");
  await expect(dialog(page).getByRole("button", { name: "Remove setup", exact: true })).toBeEnabled();
  await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
  expect(await count(page, "PurgeResidue")).toBe(0);
  expect(await count(page, "ExitUninstall")).toBe(0);
  await page.getByRole("button", { name: "Remove unfinished setup…" }).click();
  await expect(dialog(page).getByLabel("Type DELETE to confirm", { exact: true })).toHaveValue("");
});

test("the optional app removal waits for both successful native completion events", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await openRemoval(page);
  await page.evaluate(() => { window.careTest.fixtures.finishJobs = false; });
  await dialog(page).getByRole("checkbox", { name: /Also remove the CARE Clinic app/ }).check();
  await dialog(page).getByLabel("Type DELETE to confirm", { exact: true }).fill("DELETE");
  await dialog(page).getByRole("button", { name: "Delete everything", exact: true }).click();
  await expect(dialog(page)).toBeHidden();
  expect(await count(page, "RemoveApp")).toBe(0);
  await page.evaluate(() => {
    const preview = window.careTest;
    preview.state.role = "";
    preview.state.setup_done = false;
    preview.emit("uninstalled", true);
  });
  await expect(root(page)).toBeVisible();
  await expect(root(page).getByRole("button", { name: "Uninstall…", exact: true })).toBeDisabled();
  await page.evaluate(() => window.careTest.emit("care-done", 0, "status"));
  await expect(root(page)).toBeVisible();
  await expect(root(page).getByRole("button", { name: "Uninstall…", exact: true })).toBeDisabled();
  expect(await count(page, "RemoveApp")).toBe(0);
  await page.evaluate(() => window.careTest.emit("care-done", 0, "uninstall"));
  await expect.poll(() => count(page, "RemoveApp")).toBe(1);
  expect(await page.evaluate(() => window.careTest.fixtures.appRemoved)).toBe(true);
});

test("unchanged multiline custom secrets are preserved while another setting is saved", async ({ page }) => {
  await openAdvanced(page);
  await page.evaluate(() => { window.careTest.fixtures.env.backend += "CUSTOM_SECRET='first line\nsecond line'\r\n"; });
  await unlock(page);
  await group(page, "Extra settings (for support)");
  await root(page).getByRole("button", { name: "Add setting" }).click();
  await root(page).getByLabel("Setting name", { exact: true }).last().fill("CUSTOM_FEATURE");
  await root(page).getByLabel("Value for CUSTOM_FEATURE", { exact: true }).fill("enabled");
  await root(page).getByRole("button", { name: "Save changes" }).click();
  await expect(root(page)).toContainText("Settings applied.");
  expect(await page.evaluate(() => window.careTest.fixtures.env.backend.includes("CUSTOM_SECRET='first line\nsecond line'\r\n"))).toBe(true);
});

test("an external panel blocker disables the Desktop gate and guards direct form submission", async ({ page }) => {
  await openAdvanced(page);
  const password = root(page).getByLabel("CARE Clinic admin password", { exact: true });
  await password.fill(desktopPassword);
  await root(page).getByRole("button", { name: "Show care clinic admin password", exact: true }).click();
  await expect(password).toHaveAttribute("type", "text");
  await blockAdvanced(page);
  await expect(password).toBeDisabled();
  await expect(password).toHaveAttribute("type", "password");
  await expect(root(page).getByRole("button", { name: "Unlock", exact: true })).toBeDisabled();
  await expect(root(page).getByRole("button", { name: "Forgot CARE Clinic password?", exact: true })).toBeDisabled();
  await root(page).locator("form").dispatchEvent("submit");
  expect(await count(page, "VerifyAdminPassword")).toBe(0);
  await releaseAdvanced(page);
  await expect(password).toBeEnabled();
  await root(page).getByRole("button", { name: "Unlock", exact: true }).click();
  await expect(root(page).getByRole("heading", { name: "Clinic settings", exact: true })).toBeVisible();
});

for (const mode of ["reset", "change"] as const) {
  test(`an external panel blocker disables the open CARE Clinic password ${mode} form`, async ({ page }) => {
    await openAdvanced(page);
    if (mode === "change") {
      await unlock(page);
      await root(page).getByRole("button", { name: "Change password", exact: true }).click();
    } else {
      await root(page).getByRole("button", { name: "Forgot CARE Clinic password?", exact: true }).click();
      await root(page).getByLabel("Unused recovery code", { exact: true }).fill(recoveryCode);
    }
    const form = mode === "change" ? dialog(page) : root(page);
    await fillNewPassword(page, form);
    const submit = form.getByRole("button", { name: mode === "change" ? "Change CARE Clinic password" : "Reset CARE Clinic password", exact: true });
    await expect(submit).toBeEnabled();
    await blockAdvanced(page);
    await expect(submit).toBeDisabled();
    await expect(form.getByLabel("New CARE Clinic admin password", { exact: true })).toBeDisabled();
    await expect(form.getByLabel("Confirm new CARE Clinic admin password", { exact: true })).toBeDisabled();
    if (mode === "reset") await expect(form.getByLabel("Unused recovery code", { exact: true })).toBeDisabled();
    await form.locator("form").dispatchEvent("submit");
    expect(await count(page, mode === "change" ? "ChangeAdminPassword" : "ResetAdminPassword")).toBe(0);
    await form.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(page.getByLabel("New CARE Clinic admin password", { exact: true })).toHaveCount(0);
    await expect(page.getByLabel("Unused recovery code", { exact: true })).toHaveCount(0);
  });
}

for (const action of [
  { open: "New recovery codes", submit: "Choose where to save", method: "SaveAdminRecoveryCodes" },
  { open: "Rebuild", submit: "Rebuild CARE", method: "ClinicAction" },
  { open: "Uninstall…", submit: "Delete everything", method: "RunUninstall" },
] as const) {
  test(`an external panel blocker disables an already open ${action.open} dialog`, async ({ page }) => {
    await openAdvanced(page);
    await unlock(page);
    await root(page).getByRole("button", { name: action.open, exact: true }).click();
    if (action.method === "RunUninstall") await dialog(page).getByLabel("Type DELETE to confirm", { exact: true }).fill("DELETE");
    const submit = dialog(page).getByRole("button", { name: action.submit, exact: true });
    await expect(submit).toBeEnabled();
    await blockAdvanced(page);
    await expect(submit).toBeDisabled();
    if (action.method === "RunUninstall") {
      await expect(dialog(page).getByLabel("Type DELETE to confirm", { exact: true })).toBeDisabled();
      for (const checkbox of await dialog(page).getByRole("checkbox").all()) await expect(checkbox).toBeDisabled();
    }
    await submit.dispatchEvent("click");
    expect(await count(page, action.method)).toBe(0);
    expect(await count(page, "RemoveApp")).toBe(0);
    await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(dialog(page)).toBeHidden();
    for (const name of ["Change password", "New recovery codes", "Rebuild", "Uninstall…"]) {
      await expect(root(page).getByRole("button", { name, exact: true })).toBeDisabled();
    }
    await releaseAdvanced(page);
    await expect(root(page).getByRole("button", { name: action.open, exact: true })).toBeEnabled();
  });
}

for (const settings of ["Staff access", "Extra settings (for support)"]) {
  test(`an external panel blocker disables ${settings} fields and saving`, async ({ page }) => {
    await openAdvanced(page);
    await unlock(page);
    await group(page, settings);
    if (settings === "Extra settings (for support)") {
      await root(page).getByRole("button", { name: "Add setting", exact: true }).click();
      await root(page).getByLabel("Setting name", { exact: true }).last().fill("TEST_PANEL_LOCK");
      await root(page).getByLabel("Value for TEST_PANEL_LOCK", { exact: true }).fill("test-only-value");
    } else {
      await root(page).getByRole("spinbutton", { name: "Sign out inactive staff after", exact: true }).fill("20");
    }
    const save = root(page).getByRole("button", { name: "Save changes", exact: true });
    await expect(save).toBeEnabled();
    await blockAdvanced(page);
    for (const input of await root(page).locator("input").all()) await expect(input).toBeDisabled();
    if (settings === "Extra settings (for support)") {
      await expect(root(page).getByRole("button", { name: "Add setting", exact: true })).toBeDisabled();
      await expect(root(page).getByRole("button", { name: "Remove TEST_PANEL_LOCK", exact: true })).toBeDisabled();
    } else {
      await expect(root(page).getByRole("spinbutton", { name: "Sign out inactive staff after", exact: true })).toBeDisabled();
    }
    await expect(save).toBeDisabled();
    await save.dispatchEvent("click");
    expect(await count(page, "WriteEnv")).toBe(0);
    expect(await count(page, "ClinicAction")).toBe(0);
    await releaseAdvanced(page);
    await expect(save).toBeEnabled();
  });
}

test("an external panel blocker arriving during fresh reads prevents subsequent environment writes", async ({ page }) => {
  await openAdvanced(page);
  await unlock(page);
  await group(page, "Staff access");
  await root(page).getByRole("spinbutton", { name: "Sign out inactive staff after", exact: true }).fill("20");
  const before = await count(page, "ReadEnv");
  await page.evaluate(() => window.careTest.hold("ReadEnv"));
  await root(page).getByRole("button", { name: "Save changes", exact: true }).click();
  await expect.poll(() => count(page, "ReadEnv")).toBe(before + 2);
  await blockAdvanced(page);
  await page.evaluate(() => window.careTest.release("ReadEnv"));
  await expect(root(page)).toContainText("The settings couldn't be saved");
  expect(await count(page, "WriteEnv")).toBe(0);
  expect(await count(page, "ClinicAction")).toBe(0);
  await releaseAdvanced(page);
  await root(page).getByRole("button", { name: "Save changes", exact: true }).click();
  await expect(root(page)).toContainText("Settings applied.");
  expect(await count(page, "WriteEnv")).toBe(1);
  expect(await count(page, "ClinicAction")).toBe(1);
});
