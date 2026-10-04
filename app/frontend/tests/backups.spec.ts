import { mkdir } from "node:fs/promises";
import { join, relative, resolve } from "node:path";
import { expect, test, type Page } from "@playwright/test";
import type {} from "./fixtures/host";
import type { Backup } from "../src/types";
import { backupDate, backupGroups, backupProblem, backupSpaceSummary, backupWhen } from "../src/screens/panel/backup-ui";

const samplePassword = "ClinicTest123";
const root = (page: Page) => page.locator(".care-backups");
const dialog = (page: Page) => page.getByRole("alertdialog");
const replace = (page: Page) => dialog(page).getByRole("button", { name: "Replace current data", exact: true });
const password = (page: Page) => dialog(page).getByLabel("CARE Clinic admin password", { exact: true });
const acknowledge = (page: Page) => dialog(page).getByRole("checkbox", { name: "I understand today's data will be replaced" });
const callCount = (page: Page, method: string) => page.evaluate((name) =>
  window.careTest.calls.filter((call) => call.method === name).length, method);

test.use({ timezoneId: "UTC" });

test.beforeEach(({ page }) => {
  page.on("pageerror", (error) => { throw error; });
});

async function openBackups(page: Page, scenario = "panel-healthy") {
  await page.goto(`/tests/fixtures/index.html?scenario=${scenario}`);
  await page.getByRole("button", { name: "Backups", exact: true }).click();
  await expect(root(page).getByRole("button", { name: "Change folder", exact: true })).toBeEnabled();
  await expect(root(page).getByText("Kept forever", { exact: true })).toBeVisible();
}

test("backup key re-download works with only the password after the original PEM is deleted", async ({ page }) => {
  await openBackups(page);
  await page.evaluate(() => {
    window.careTest.fixtures.recovery.backup_path = "";
    window.careTest.fixtures.recovery.backup_problem = "missing";
    window.careTest.fixtures.recoveryFile = "";
  });
  await root(page).getByRole("button", { name: "Re-download backup key", exact: true }).click();
  const save = dialog(page).getByRole("button", { name: "Choose where to save", exact: true });
  await expect(save).toBeDisabled();
  await expect(dialog(page)).toContainText("the original PEM is not needed");
  await password(page).fill("incorrect");
  await save.click();
  await expect(dialog(page).getByRole("alert")).toContainText("password didn't match");
  await expect(password(page)).toHaveValue("");
  await password(page).fill(samplePassword);
  await save.click();
  await expect(dialog(page)).toBeHidden();
  await expect(root(page).getByRole("status")).toContainText("The key is unchanged");
  expect(await page.evaluate(() => window.careTest.calls.filter((call) => call.method === "ExportBackupRecovery").map((call) => call.args)))
    .toEqual([["incorrect", ""], [samplePassword, ""]]);
  expect(await callCount(page, "ReplaceSetupBackupRecovery")).toBe(0);
  expect(await callCount(page, "ChooseRecoveryFile")).toBe(0);
  await root(page).getByRole("button", { name: "Re-download backup key", exact: true }).click();
  await expect(password(page)).toHaveValue("");
});

test("backup key export handles lost originals, alternate copies and cancellation", async ({ page }) => {
  await openBackups(page);
  await page.evaluate(() => { window.careTest.fixtures.recovery.backup_key_stored = false; });
  await root(page).getByRole("button", { name: "Re-download backup key", exact: true }).click();
  await password(page).fill(samplePassword);
  const save = dialog(page).getByRole("button", { name: "Choose where to save", exact: true });
  await save.click();
  await expect(dialog(page).getByRole("alert")).toContainText("cannot reconstruct a key lost before enrollment");
  await dialog(page).getByRole("button", { name: "Select another saved copy", exact: true }).click();
  await page.evaluate(() => window.careTest.respond("ExportBackupRecovery", false));
  await password(page).fill(samplePassword);
  await save.click();
  await expect(dialog(page)).toContainText("No file was saved");
  await expect(password(page)).toHaveValue("");
  expect(await page.evaluate(() => window.careTest.calls.filter((call) => call.method === "ExportBackupRecovery").slice(-1)[0]?.args))
    .toEqual([samplePassword, await page.evaluate(() => window.careTest.fixtures.recoveryFile)]);
  await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
  await root(page).getByRole("button", { name: "Re-download backup key", exact: true }).click();
  await expect(dialog(page)).toContainText("Using the original saved recovery file location");
  expect(await page.evaluate(() => window.careTest.fixtures.recovery.backup_key_stored)).toBe(false);
});

test("legacy backup key enrollment enables future downloads without the source PEM", async ({ page }) => {
  await openBackups(page);
  await page.evaluate(() => { window.careTest.fixtures.recovery.backup_key_stored = false; });
  const trigger = root(page).getByRole("button", { name: "Re-download backup key", exact: true });
  await trigger.click();
  await expect(dialog(page)).toContainText("Enable password-only downloads");
  await dialog(page).getByRole("button", { name: "Select another saved copy", exact: true }).click();
  await password(page).fill(samplePassword);
  await dialog(page).getByRole("button", { name: "Choose where to save", exact: true }).click();
  await expect(dialog(page)).toBeHidden();
  expect(await page.evaluate(() => window.careTest.fixtures.recovery.backup_key_stored)).toBe(true);
  await page.evaluate(() => {
    window.careTest.fixtures.recoveryFile = "";
    window.careTest.fixtures.recovery.backup_path = "";
  });
  await trigger.click();
  await expect(dialog(page)).toContainText("the original PEM is not needed");
  await password(page).fill(samplePassword);
  await dialog(page).getByRole("button", { name: "Choose where to save", exact: true }).click();
  await expect(dialog(page)).toBeHidden();
  expect(await callCount(page, "ExportBackupRecovery")).toBe(2);
  expect(await callCount(page, "ChooseRecoveryFile")).toBe(1);
  expect(await callCount(page, "ReplaceSetupBackupRecovery")).toBe(0);
});

test("backup key export explains re-enrollment after a forgotten-password reset", async ({ page }) => {
  await openBackups(page);
  await page.evaluate(() => {
    window.careTest.fixtures.recovery.backup_key_stored = false;
    window.careTest.fixtures.recovery.backup_key_needs_enrollment = true;
  });
  await root(page).getByRole("button", { name: "Re-download backup key", exact: true }).click();
  await expect(dialog(page)).toContainText("Re-enroll after your password reset");
  await dialog(page).getByRole("button", { name: "Select another saved copy", exact: true }).click();
  await password(page).fill(samplePassword);
  await dialog(page).getByRole("button", { name: "Choose where to save", exact: true }).click();
  await expect(dialog(page)).toBeHidden();
  expect(await page.evaluate(() => window.careTest.fixtures.recovery.backup_key_needs_enrollment)).toBe(false);
});

test("backup key export rejects duplicate submissions and reports incompatible keys", async ({ page }) => {
  await openBackups(page);
  await root(page).getByRole("button", { name: "Re-download backup key", exact: true }).click();
  await password(page).fill(samplePassword);
  await page.evaluate(() => {
    window.careTest.hold("ExportBackupRecovery");
    window.careTest.failNext("ExportBackupRecovery", "the selected recovery file does not match this clinic's configured backup key");
  });
  await dialog(page).getByRole("button", { name: "Choose where to save", exact: true }).dblclick();
  await expect.poll(() => callCount(page, "ExportBackupRecovery")).toBe(1);
  await expect(dialog(page).getByRole("button", { name: "Cancel", exact: true })).toBeDisabled();
  await page.keyboard.press("Escape");
  await expect(dialog(page)).toBeVisible();
  await page.evaluate(() => window.careTest.release("ExportBackupRecovery"));
  await expect(dialog(page).getByRole("alert")).toContainText("doesn't match this clinic");
  await expect(password(page)).toHaveValue("");
  expect(await callCount(page, "ReplaceSetupBackupRecovery")).toBe(0);
});

test("backup key re-download is locked during an application update", async ({ page }) => {
  await openBackups(page);
  const trigger = root(page).getByRole("button", { name: "Re-download backup key", exact: true });
  await trigger.click();
  await password(page).fill(samplePassword);
  await page.evaluate(() => window.careTest.progress({ phase: "downloading", done: 1, total: 100 }));
  await expect(password(page)).toHaveValue("");
  await expect(dialog(page).getByRole("button", { name: "Choose where to save", exact: true })).toBeDisabled();
  await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(trigger).toBeDisabled();
  expect(await callCount(page, "ExportBackupRecovery")).toBe(0);
});

async function chooseBackup(page: Page) {
  await root(page).getByRole("button", { name: "Choose file", exact: true }).click();
  await expect(dialog(page)).toBeVisible();
  await expect(replace(page)).toBeDisabled();
}

async function fillRestore(page: Page, encrypted = true) {
  if (encrypted) await dialog(page).getByRole("button", { name: "Choose recovery file", exact: true }).click();
  await password(page).fill(samplePassword);
  await acknowledge(page).check();
  await expect(replace(page)).toBeEnabled();
}

async function refresh(page: Page) {
  await root(page).getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(root(page).getByRole("button", { name: "Refresh", exact: true })).toBeEnabled();
}

async function updateHandoff(page: Page, phase: "installer" | "restarting" = "installer") {
  await page.evaluate((phase) => {
    window.careTest.progress({ phase, done: 50_000_000, total: 50_000_000 });
    window.careTest.finishUpdate();
  }, phase);
}

async function acknowledgeUpdate(page: Page) {
  const navigation = page.getByRole("navigation", { name: "Clinic sections" });
  await navigation.getByRole("button", { name: "Updates", exact: true }).click();
  await page.getByRole("region", { name: "CARE Clinic application", exact: true })
    .getByRole("button", { name: "Done", exact: true }).click();
  await navigation.getByRole("button", { name: "Backups", exact: true }).click();
}

async function capture(page: Page, name: string) {
  if (!process.env.CARE_SCREENSHOTS_DIR) return;
  const folder = relative(process.cwd(), resolve(process.env.CARE_SCREENSHOTS_DIR));
  await mkdir(folder, { recursive: true });
  await page.evaluate(() => document.fonts.ready);
  await page.screenshot({ path: join(folder, `${name}.png`), animations: "disabled" });
}

async function assertFits(page: Page) {
  expect(await page.evaluate(() => {
    const backups = document.querySelector(".care-backups")!;
    const modal = document.querySelector(".care-restore-dialog");
    const box = modal?.getBoundingClientRect();
    return {
      pageX: document.documentElement.scrollWidth > innerWidth,
      pageY: document.documentElement.scrollHeight > innerHeight,
      backupsX: backups.scrollWidth > backups.clientWidth + 1,
      dialogX: !!modal && modal.scrollWidth > modal.clientWidth + 1,
      dialogWithinWindow: !box || (box.left >= 0 && box.right <= innerWidth && box.top >= 0 && box.bottom <= innerHeight + 1),
    };
  })).toEqual({ pageX: false, pageY: false, backupsX: false, dialogX: false, dialogWithinWindow: true });
}

const boardBackups: Backup[] = [
  { db_dump: "care-20261001-020000.dump.enc", files_archive: "files-20261001-020000.tar.gz.enc", label: "", manual: false, encrypted: true, size_bytes: 1.2 * 2 ** 30 },
  { db_dump: "care-20260930-020000.dump.enc", files_archive: "files-20260930-020000.tar.gz.enc", label: "", manual: false, encrypted: true, size_bytes: 1.2 * 2 ** 30 },
  { db_dump: "care-manual-20260930-114700.dump.enc", files_archive: "", label: "", manual: true, encrypted: true, size_bytes: 0.9 * 2 ** 30 },
  { db_dump: "care-20260927-231000.dump.enc", files_archive: "files-20260927-231000.tar.gz.enc", label: "", manual: false, encrypted: true, size_bytes: 1.2 * 2 ** 30 },
  { db_dump: "care-20260926-231000.dump.enc", files_archive: "files-20260926-231000.tar.gz.enc", label: "", manual: false, encrypted: true, size_bytes: 1.1 * 2 ** 30 },
  { db_dump: "care-20260925-231000.dump.enc", files_archive: "files-20260925-231000.tar.gz.enc", label: "", manual: false, encrypted: true, size_bytes: 1.1 * 2 ** 30 },
];

for (const viewport of [{ width: 1100, height: 700 }, { width: 720, height: 560 }]) {
  test(`Backups and restore fit ${viewport.width}x${viewport.height}`, async ({ page }) => {
    await page.setViewportSize(viewport);
    await page.clock.setFixedTime(new Date("2026-10-01T12:00:00Z"));
    await openBackups(page);
    await page.evaluate((backups) => {
      const preview = window.careTest;
      preview.fixtures.backups = backups;
      preview.fixtures.storage.backup = {
        ...preview.fixtures.storage.backup, total: 1_000 * 2 ** 30, need: 1.3 * 2 ** 30,
        days_left: 1_100,
      };
      preview.fixtures.storage.last_run.at = Date.parse("2026-10-01T02:00:00Z") / 1000;
      preview.fixtures.storage.newest_backup_at = preview.fixtures.storage.last_run.at;
      preview.fixtures.importedBackup = {
        ...preview.fixtures.importedBackup, db_dump: backups[0].db_dump,
        path: `${preview.fixtures.backupDir}/${backups[0].db_dump}`,
        files_archive: backups[0].files_archive,
      };
    }, boardBackups);
    await refresh(page);
    await expect(root(page).getByRole("heading", { name: "Today", exact: true })).toBeVisible();
    await expect(root(page).getByRole("heading", { name: "Yesterday", exact: true })).toBeVisible();
    await expect(root(page).getByRole("heading", { name: "Today, 02:00", exact: true })).toBeVisible();
    expect(await root(page).locator(".care-backups-list button").count()).toBe(0);
    await root(page).evaluate((element) => {
      for (let parent = element.parentElement; parent; parent = parent.parentElement) parent.scrollTop = 0;
    });
    await assertFits(page);
    await capture(page, `backups-${viewport.width}x${viewport.height}`);
    await chooseBackup(page);
    await expect(dialog(page)).toContainText("Changes made since this backup will be lost");
    await expect(dialog(page)).not.toContainText("few minutes");
    await assertFits(page);
    await capture(page, `restore-${viewport.width}x${viewport.height}`);
    await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(dialog(page)).toBeHidden();
    await expect(root(page).getByRole("button", { name: "Restore this file", exact: true })).toBeEnabled();
  });
}

test("schedule and retention use native settings, not an invented clock", async ({ page }) => {
  await openBackups(page);
  await expect(root(page)).toContainText("Automatic every 24 hours");
  await page.evaluate(() => { window.careTest.fixtures.backupPolicy.retention_days = 7; });
  await refresh(page);
  await expect(root(page).getByText("Kept for 7 days", { exact: true })).toBeVisible();
  await expect(root(page).getByText("Kept forever", { exact: true })).toHaveCount(0);
  await expect(root(page)).not.toContainText("every night");
  await page.evaluate(() => window.careTest.failNext("GetBackupPolicy", "couldn't read backend.env: private setting details"));
  await refresh(page);
  await expect(root(page).getByRole("alert")).toContainText("The backup settings couldn't be read");
  await expect(root(page)).not.toContainText("Kept forever");
  await expect(root(page)).not.toContainText("private setting details");
});

test("empty and unreadable backup lists are distinct", async ({ page }) => {
  await openBackups(page, "panel-no-backups");
  await expect(root(page).getByRole("heading", { name: "No backups yet", exact: true })).toBeVisible();
  await page.evaluate(() => window.careTest.failNext("ListBackups", "read directory EACCES /private/clinic"));
  await refresh(page);
  await expect(root(page).getByRole("alert")).toContainText("The backup list couldn't be read");
  await expect(root(page).getByRole("heading", { name: "No backups yet", exact: true })).toHaveCount(0);
  await expect(root(page)).not.toContainText("EACCES");
  await expect(root(page).getByRole("button", { name: "Choose file", exact: true })).toBeEnabled();
});

test("storage and staleness use report fields without guessing a drive type", async ({ page }) => {
  await openBackups(page);
  await page.evaluate(() => {
    const report = window.careTest.fixtures.storage;
    report.stale = true;
    report.newest_backup_at = Math.floor(Date.now() / 1000) - 3 * 86400;
    report.backup = { ...report.backup, level: "unknown", free: 0, total: 0, message: "cannot stat /Volumes/USB: EIO" };
  });
  await refresh(page);
  await expect(root(page)).toContainText("No backup since");
  await expect(root(page)).toContainText("Free space couldn't be checked");
  await expect(root(page)).not.toContainText("USB");
  await expect(root(page)).not.toContainText("EIO");
  await expect(root(page)).not.toContainText("Not connected");
  await expect(root(page).getByRole("meter")).toHaveCount(0);
});

test("failed-backup facts are visible but raw causes stay in logs", async ({ page }) => {
  await openBackups(page);
  await page.evaluate(() => {
    const report = window.careTest.fixtures.storage;
    report.last_run = { ...report.last_run, state: "failed", reason: "disk_full", need_bytes: 1.3 * 2 ** 30, free_bytes: 0.4 * 2 ** 30, message: "native command failed: exit status 1" };
    report.backup = { ...report.backup, level: "critical", free: 0.4 * 2 ** 30, need: 1.3 * 2 ** 30 };
  });
  await refresh(page);
  await expect(root(page)).toContainText("It needed about 1.3 GB and 410 MB was free");
  await expect(root(page)).toContainText("Not enough room for the next backup");
  await expect(root(page)).not.toContainText("exit status");
  await expect(root(page).getByRole("button", { name: "Choose another folder" })).toBeEnabled();
  await root(page).getByRole("button", { name: "Open log file for support" }).click();
  expect(await callCount(page, "OpenLogFolder")).toBe(1);
});

for (const [native, title] of [
  ["That folder already holds backups from another CARE installation. /private/old", "This folder belongs to another clinic"],
  ["Keep the backup recovery file separate from the backup folder.", "Keep the recovery file separate"],
  ["choose a location outside /private/CARE/logs", "Choose a folder outside CARE"],
  ["That drive is read-only. NTFS /private/device", "That folder is read-only"],
  ["This computer isn't allowed to write to that folder.", "CARE can't write to that folder"],
  ["Not enough room for the next backup. Need more free space.", "There isn't enough room in that folder"],
]) {
  test(`folder validation: ${title}`, async ({ page }) => {
    await openBackups(page);
    const previous = await root(page).locator(".care-backups-folder-path").innerText();
    await page.evaluate((message) => {
      window.careTest.fixtures.folder = "/test-fixtures/new-location";
      window.careTest.fixtures.folderProblem = message;
    }, native);
    await root(page).getByRole("button", { name: "Change folder", exact: true }).click();
    await expect(root(page).getByRole("alert")).toContainText(title);
    await expect(root(page).locator(".care-backups-folder-path")).toHaveText(previous);
    await expect(root(page)).not.toContainText("/private/");
    await expect(root(page)).not.toContainText("NTFS");
    await expect(root(page)).not.toContainText("The backup folder has changed");
  });
}

test("changing folders is single-flight and does not imply moving older backups", async ({ page }) => {
  await openBackups(page);
  await page.evaluate(() => {
    window.careTest.fixtures.folder = "/test-fixtures/new-location";
    window.careTest.hold("SetBackupDir");
  });
  await root(page).getByRole("button", { name: "Change folder", exact: true })
    .evaluate((button: HTMLButtonElement) => { button.click(); button.click(); });
  await expect(root(page).getByRole("button", { name: "Changing folder…" })).toBeDisabled();
  expect(await callCount(page, "ChooseFolder")).toBe(1);
  expect(await callCount(page, "SetBackupDir")).toBe(1);
  await expect(root(page).getByRole("button", { name: "Back up now", exact: true })).toBeDisabled();
  await page.evaluate(() => window.careTest.release("SetBackupDir"));
  await expect(root(page).locator(".care-backups-folder-path")).toHaveText("/test-fixtures/new-location/care-db-backups");
  await expect(root(page)).toContainText("Earlier backups stay in the previous folder");
  await page.evaluate(() => window.careTest.respond("ChooseFolder", ""));
  await root(page).getByRole("button", { name: "Change folder", exact: true }).click();
  expect(await callCount(page, "SetBackupDir")).toBe(1);
});

test("a backup is not successful until the native job finishes", async ({ page }) => {
  await openBackups(page);
  const before = await root(page).locator(".care-backups-list li").count();
  await page.evaluate(() => { window.careTest.fixtures.finishJobs = false; });
  await root(page).getByRole("button", { name: "Back up now", exact: true })
    .evaluate((button: HTMLButtonElement) => { button.click(); button.click(); });
  await expect(root(page)).toContainText("A backup is running");
  await expect(root(page)).not.toContainText("The backup finished");
  expect(await root(page).locator(".care-backups-list li").count()).toBe(before);
  expect(await page.evaluate(() => window.careTest.calls.filter((call) =>
    call.method === "ClinicAction" && call.args[0] === "backup-now").length)).toBe(1);
  await expect(root(page).getByRole("button", { name: "Choose file", exact: true })).toBeDisabled();
  await page.evaluate(() => window.careTest.finishJob("backup-now"));
  await expect(root(page)).toContainText("The backup finished");
  await expect(root(page).locator(".care-backups-list li")).toHaveCount(before + 1);
});

test("an accepted backup that later fails never adds a successful copy", async ({ page }) => {
  await openBackups(page);
  const before = await root(page).locator(".care-backups-list li").count();
  const previousRun = await page.evaluate(() => {
    window.careTest.fixtures.finishJobs = false;
    return { ...window.careTest.fixtures.storage.last_run };
  });
  await root(page).getByRole("button", { name: "Back up now", exact: true }).click();
  await expect(root(page)).toContainText("A backup is running");
  await page.evaluate((previous) => {
    window.careTest.fixtures.storage.last_run = previous;
    window.careTest.finishJob("backup-now", "failed database copy: exit status 42 /private/clinic");
  }, previousRun);
  await expect(page.locator(".panel-banner[role='alert']")).toContainText("Backup didn't finish");
  await expect(root(page)).not.toContainText("The backup finished");
  await expect(root(page).locator(".care-backups-list li")).toHaveCount(before);
  await expect(page.locator("body")).not.toContainText("exit status 42");
});

test("backup rejection and storage recheck errors are actionable", async ({ page }) => {
  await openBackups(page);
  await page.evaluate(() => window.careTest.failNext("ClinicAction", "native command exited 71: /private/backup"));
  await root(page).getByRole("button", { name: "Back up now", exact: true }).click();
  await expect(page.locator(".panel-banner[role='alert']")).toContainText("Backup didn't finish");
  await expect(root(page)).not.toContainText("exited 71");
  await page.evaluate(() => window.careTest.failNext("RecheckStorage", "cannot stat /private/clinic"));
  await refresh(page);
  await expect(root(page).getByRole("alert")).toContainText("Storage couldn't be checked");
});

for (const phase of ["installer", "restarting"] as const) {
  test(`update ${phase} handoff locks Backups until ${phase === "installer" ? "Done acknowledgement" : "reopening"}`, async ({ page }) => {
    await openBackups(page);
    await chooseBackup(page);
    await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
    await updateHandoff(page, phase);
    const controls = ["Back up now", "Change folder", "Refresh", "Choose a different file", "Restore this file"];
    for (const name of controls) {
      await expect(root(page).getByRole("button", { name, exact: true })).toBeDisabled();
    }
    await page.getByRole("navigation", { name: "Clinic sections" })
      .getByRole("button", { name: "Overview", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
    expect(await callCount(page, "ClinicAction")).toBe(0);
    expect(await callCount(page, "SetBackupDir")).toBe(0);
    expect(await callCount(page, "RestoreFromFile")).toBe(0);
    if (phase === "restarting") {
      await page.getByRole("navigation", { name: "Clinic sections" })
        .getByRole("button", { name: "Updates", exact: true }).click();
      await expect(page.getByRole("region", { name: "CARE Clinic application", exact: true })).toContainText("CARE Clinic is reopening.");
      await expect(page.getByRole("button", { name: "Done", exact: true })).toHaveCount(0);
      return;
    }
    await acknowledgeUpdate(page);
    for (const name of controls) {
      await expect(root(page).getByRole("button", { name, exact: true })).toBeEnabled();
    }
    await page.evaluate(() => { window.careTest.fixtures.finishJobs = false; });
    await root(page).getByRole("button", { name: "Back up now", exact: true }).click();
    expect(await callCount(page, "ClinicAction")).toBe(1);
    await page.evaluate(() => window.careTest.finishJob("backup-now"));
    await expect(root(page)).toContainText("The backup finished");
  });
}

test("live update lock blocks Backups actions before the disabled render", async ({ page }) => {
  await openBackups(page);
  await chooseBackup(page);
  await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
  const methods = ["ClinicAction", "ChooseFolder", "SetBackupDir", "ChooseBackupFile", "InspectBackupFile", "RecheckStorage"];
  const before = await Promise.all(methods.map((method) => callCount(page, method)));
  await page.evaluate(() => {
    window.careTest.progress({ phase: "verifying", done: 50_000_000, total: 50_000_000 });
    const buttons = Array.from(document.querySelectorAll<HTMLButtonElement>(".care-backups button"));
    for (const name of ["Back up now", "Change folder", "Refresh", "Choose a different file", "Restore this file"]) {
      const button = buttons.find((button) => button.textContent?.trim() === name);
      if (!button) throw new Error(`Missing backup action: ${name}`);
      button.click();
    }
  });
  await expect(root(page).getByRole("button", { name: "Back up now", exact: true })).toBeDisabled();
  await expect(dialog(page)).toBeHidden();
  expect(await Promise.all(methods.map((method) => callCount(page, method)))).toEqual(before);
});

test("live update lock blocks restore submission while Cancel and recovery navigation stay usable", async ({ page }) => {
  await openBackups(page);
  await chooseBackup(page);
  await fillRestore(page);
  const inspections = await callCount(page, "InspectBackupFile");
  await page.evaluate(() => {
    window.careTest.progress({ phase: "verifying", done: 50_000_000, total: 50_000_000 });
    document.querySelector<HTMLFormElement>(".care-restore-dialog form")!.requestSubmit();
  });
  await expect(replace(page)).toBeDisabled();
  await expect(dialog(page)).toContainText("Finish the CARE Clinic update first");
  await expect(dialog(page).getByRole("button", { name: "Cancel", exact: true })).toBeEnabled();
  expect(await callCount(page, "InspectBackupFile")).toBe(inspections);
  expect(await callCount(page, "VerifyAdminPassword")).toBe(0);
  expect(await callCount(page, "RestoreFromFile")).toBe(0);
  await dialog(page).getByRole("button", { name: "Forgot CARE Clinic password?", exact: true }).click();
  await expect(dialog(page)).toBeHidden();
  await expect(page.getByRole("heading", { name: "Advanced", exact: true })).toBeVisible();
  await updateHandoff(page);
  await acknowledgeUpdate(page);
  await root(page).getByRole("button", { name: "Restore this file", exact: true }).click();
  await expect(password(page)).toHaveValue("");
  await expect(acknowledge(page)).not.toBeChecked();
});

for (const releaseAfterAcknowledgement of [false, true]) {
  test(`update handoff invalidates a held folder picker ${releaseAfterAcknowledgement ? "after" : "before"} acknowledgement`, async ({ page }) => {
    await openBackups(page);
    const previous = await root(page).locator(".care-backups-folder-path").innerText();
    await page.evaluate(() => {
      window.careTest.fixtures.folder = "/test-fixtures/new-backup-location";
      window.careTest.hold("ChooseFolder");
    });
    await root(page).getByRole("button", { name: "Change folder", exact: true }).click();
    await expect.poll(() => callCount(page, "ChooseFolder")).toBe(1);
    await updateHandoff(page);
    if (releaseAfterAcknowledgement) await acknowledgeUpdate(page);
    await page.evaluate(() => window.careTest.release("ChooseFolder"));
    await expect(root(page).getByRole("button", { name: "Changing folder…", exact: true })).toHaveCount(0);
    expect(await callCount(page, "SetBackupDir")).toBe(0);
    await expect(root(page).locator(".care-backups-folder-path")).toHaveText(previous);
    if (!releaseAfterAcknowledgement) await acknowledgeUpdate(page);
    await root(page).getByRole("button", { name: "Change folder", exact: true }).click();
    await expect(root(page)).toContainText("The backup folder has changed");
    expect(await callCount(page, "SetBackupDir")).toBe(1);
  });
}

for (const method of ["ChooseBackupFile", "InspectBackupFile"] as const) {
  test(`update handoff rejects stale backup selection from held ${method}`, async ({ page }) => {
    await openBackups(page);
    await chooseBackup(page);
    await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
    const previous = await root(page).locator(".care-backups-selected strong").innerText();
    const before = await callCount(page, method);
    await page.evaluate((method) => {
      const fixture = window.careTest.fixtures.importedBackup;
      fixture.db_dump = "care-20261004-020004.dump.enc";
      fixture.path = `${fixture.dir}/${fixture.db_dump}`;
      window.careTest.hold(method);
    }, method);
    await root(page).getByRole("button", { name: "Choose a different file", exact: true }).click();
    await expect.poll(() => callCount(page, method)).toBe(before + 1);
    const inspections = await callCount(page, "InspectBackupFile");
    await updateHandoff(page);
    await page.evaluate((method) => window.careTest.release(method), method);
    await expect(root(page).getByRole("button", { name: "Choose a different file", exact: true })).toBeDisabled();
    await expect(root(page).locator(".care-backups-selected strong")).toHaveText(previous);
    await expect(dialog(page)).toBeHidden();
    expect(await callCount(page, "InspectBackupFile")).toBe(inspections);
    expect(await callCount(page, "RestoreFromFile")).toBe(0);
    await acknowledgeUpdate(page);
    await root(page).getByRole("button", { name: "Choose a different file", exact: true }).click();
    await expect(dialog(page).getByText("care-20261004-020004.dump.enc", { exact: true })).toBeVisible();
  });
}

test("update handoff permits cancelling a held recovery picker without replacing the saved selection", async ({ page }) => {
  await openBackups(page);
  await chooseBackup(page);
  await fillRestore(page);
  const previous = await page.evaluate(() => window.careTest.fixtures.recoveryFile);
  await page.evaluate(() => {
    window.careTest.fixtures.recoveryFile = "/test-fixtures/recovery/different-recovery.pem";
    window.careTest.hold("ChooseRecoveryFile");
  });
  await dialog(page).getByRole("button", { name: "Choose recovery file", exact: true }).click();
  await expect.poll(() => callCount(page, "ChooseRecoveryFile")).toBe(2);
  await updateHandoff(page);
  await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(dialog(page)).toBeHidden();
  await acknowledgeUpdate(page);
  await page.evaluate(() => window.careTest.release("ChooseRecoveryFile"));
  await root(page).getByRole("button", { name: "Restore this file", exact: true }).click();
  await expect(dialog(page).getByText(previous, { exact: true })).toBeVisible();
  await expect(password(page)).toHaveValue("");
  await expect(acknowledge(page)).not.toBeChecked();
  expect(await callCount(page, "RestoreFromFile")).toBe(0);
});

for (const method of ["InspectBackupFile", "VerifyAdminPassword"] as const) {
  test(`update handoff cancels restore preflight held at ${method} without restarting after acknowledgement`, async ({ page }) => {
    await openBackups(page);
    await chooseBackup(page);
    await fillRestore(page);
    const before = await callCount(page, method);
    await page.evaluate((method) => {
      window.careTest.fixtures.finishJobs = false;
      window.careTest.hold(method);
    }, method);
    await replace(page).click();
    await expect.poll(() => callCount(page, method)).toBe(before + 1);
    await updateHandoff(page);
    await expect(dialog(page).getByRole("button", { name: "Cancel", exact: true })).toBeEnabled();
    await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(dialog(page)).toBeHidden();
    await acknowledgeUpdate(page);
    await page.evaluate((method) => window.careTest.release(method), method);
    await expect(root(page).getByRole("button", { name: "Restore this file", exact: true })).toBeEnabled();
    expect(await callCount(page, "VerifyAdminPassword")).toBe(method === "VerifyAdminPassword" ? 1 : 0);
    expect(await callCount(page, "RestoreFromFile")).toBe(0);
    await root(page).getByRole("button", { name: "Restore this file", exact: true }).click();
    await expect(password(page)).toHaveValue("");
    await expect(acknowledge(page)).not.toBeChecked();
    await fillRestore(page, false);
    await replace(page).click();
    await expect(dialog(page)).toBeHidden();
    expect(await callCount(page, "RestoreFromFile")).toBe(1);
    await page.evaluate(() => window.careTest.finishJob("restore"));
    await expect(root(page)).toContainText("The backup was restored");
  });
}

test("encrypted restore requires the file, password and real acknowledgement", async ({ page }) => {
  await openBackups(page);
  await chooseBackup(page);
  await password(page).fill(samplePassword);
  await acknowledge(page).check();
  await expect(replace(page)).toBeDisabled();
  await password(page).press("Enter");
  expect(await callCount(page, "RestoreFromFile")).toBe(0);
  await dialog(page).getByRole("button", { name: "Choose recovery file", exact: true }).click();
  await acknowledge(page).uncheck();
  await expect(replace(page)).toBeDisabled();
  await password(page).press("Enter");
  expect(await callCount(page, "RestoreFromFile")).toBe(0);
  await acknowledge(page).check();
  await expect(replace(page)).toBeEnabled();
});

test("wrong passwords stay in the confirmation without starting a restore", async ({ page }) => {
  await openBackups(page);
  await chooseBackup(page);
  await fillRestore(page);
  await password(page).fill("WrongPreview123");
  await replace(page).click();
  await expect(dialog(page).getByRole("alert")).toContainText("That CARE Clinic admin password doesn't match");
  await expect(password(page)).toHaveValue("WrongPreview123");
  await expect(acknowledge(page)).toBeChecked();
  expect(await callCount(page, "RestoreFromFile")).toBe(0);
  expect(await page.evaluate(() => window.careTest.logs.join("\n"))).not.toContain("WrongPreview123");
});

test("cancelling recovery selection keeps the form and accepted restoration clears passwords", async ({ page }) => {
  await openBackups(page);
  await page.evaluate(() => { window.careTest.fixtures.finishJobs = false; });
  await chooseBackup(page);
  await password(page).fill(samplePassword);
  await acknowledge(page).check();
  await page.evaluate(() => window.careTest.respond("ChooseRecoveryFile", ""));
  await dialog(page).getByRole("button", { name: "Choose recovery file", exact: true }).click();
  await expect(password(page)).toHaveValue(samplePassword);
  await expect(acknowledge(page)).toBeChecked();
  await expect(replace(page)).toBeDisabled();
  await page.evaluate(() => window.careTest.respond("ChooseRecoveryFile", window.careTest.fixtures.recoveryFile));
  await dialog(page).getByRole("button", { name: "Choose recovery file", exact: true }).click();
  await replace(page).evaluate((button: HTMLButtonElement) => { button.click(); button.click(); });
  await expect(dialog(page)).toBeHidden();
  await expect(root(page)).toContainText("Restoring the selected backup");
  await expect(root(page)).not.toContainText("The backup was restored");
  expect(await callCount(page, "RestoreFromFile")).toBe(1);
  await page.evaluate(() => window.careTest.finishJob("restore"));
  await expect(root(page)).toContainText("The backup was restored");
  await root(page).getByRole("button", { name: "Restore this file", exact: true }).click();
  await expect(password(page)).toHaveValue("");
  await expect(acknowledge(page)).not.toBeChecked();
  expect(await page.evaluate(() => window.careTest.logs.join("\n"))).not.toContain(samplePassword);
});

test("cancelling a different backup selection keeps the previous valid file", async ({ page }) => {
  await openBackups(page);
  await chooseBackup(page);
  const previous = await dialog(page).locator(".care-restore-file strong").innerText();
  await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
  await page.evaluate(() => window.careTest.respond("ChooseBackupFile", ""));
  await root(page).getByRole("button", { name: "Choose a different file", exact: true }).click();
  await expect(dialog(page)).toBeHidden();
  await expect(root(page).locator(".care-backups-selected strong")).toHaveText(previous);
  await expect(root(page).getByRole("button", { name: "Restore this file", exact: true })).toBeEnabled();
  expect(await callCount(page, "InspectBackupFile")).toBe(1);
});

test("synchronous restore rejection preserves the selection and never claims success", async ({ page }) => {
  await openBackups(page);
  await chooseBackup(page);
  await fillRestore(page);
  await page.evaluate(() => window.careTest.failNext("RestoreFromFile", "the recovery file does not match this backup"));
  await replace(page).click();
  await expect(dialog(page)).toBeVisible();
  await expect(dialog(page).getByRole("alert")).toContainText("recovery file");
  await expect(password(page)).toHaveValue(samplePassword);
  await expect(acknowledge(page)).toBeChecked();
  await expect(replace(page)).toBeEnabled();
  await expect(root(page)).not.toContainText("The backup was restored");
  expect(await callCount(page, "RestoreFromFile")).toBe(1);
});

test("restore failure retains recovery gates without exposing raw native output", async ({ page }) => {
  await openBackups(page);
  await page.evaluate(() => { window.careTest.fixtures.finishJobs = false; });
  await chooseBackup(page);
  await fillRestore(page);
  await replace(page).click();
  await expect(dialog(page)).toBeHidden();
  await page.evaluate(() => {
    window.careTest.state.restore_pending = true;
    window.careTest.finishJob("restore", "backup copy, decryption or full dump validation failed: exit status 37 /private/key");
  });
  await expect(root(page)).toContainText("An earlier restore needs attention");
  await expect(root(page).getByRole("button", { name: "Back up now", exact: true })).toBeDisabled();
  await expect(root(page).getByRole("button", { name: "Change folder", exact: true })).toBeDisabled();
  await expect(root(page).getByRole("button", { name: "Restore this file", exact: true })).toBeDisabled();
  await expect(page.locator("body")).not.toContainText("exit status 37");
  await expect(page.locator("body")).not.toContainText("/private/key");
  await expect(root(page)).not.toContainText("The backup was restored");
});

for (const native of [
  "that isn't a CARE database backup",
  "couldn't open that file: no such file",
  "choose a regular backup file",
  "backup must be a nonempty regular file, not a directory or link",
]) {
  test(`invalid or missing backup is rejected: ${native}`, async ({ page }) => {
    await openBackups(page);
    await page.evaluate((error) => window.careTest.failNext("InspectBackupFile", error), native);
    await root(page).getByRole("button", { name: "Choose file", exact: true }).click();
    await expect(root(page).getByRole("alert")).toBeVisible();
    await expect(dialog(page)).toBeHidden();
    expect(await callCount(page, "RestoreFromFile")).toBe(0);
  });
}

test("missing or invalid recovery files do not enable restoration", async ({ page }) => {
  await openBackups(page);
  await chooseBackup(page);
  await password(page).fill(samplePassword);
  await acknowledge(page).check();
  await page.evaluate(() => window.careTest.failNext("ChooseRecoveryFile", "the backup recovery file is damaged: ASN1 parse failed"));
  await dialog(page).getByRole("button", { name: "Choose recovery file", exact: true }).click();
  await expect(dialog(page).getByRole("alert")).toContainText("That recovery file couldn't be used");
  await expect(replace(page)).toBeDisabled();
  await expect(dialog(page)).not.toContainText("ASN1");
  expect(await callCount(page, "RestoreFromFile")).toBe(0);
});

test("legacy database-only backups do not ask for an unrelated recovery file", async ({ page }) => {
  await page.setViewportSize({ width: 720, height: 560 });
  await openBackups(page);
  await page.evaluate(() => {
    const preview = window.careTest;
    preview.fixtures.importedBackup = {
      ...preview.fixtures.importedBackup, encrypted: false, files_archive: "",
      db_dump: "care-20260930-020000.dump",
      path: `${preview.fixtures.backupDir}/care-20260930-020000.dump`,
    };
    preview.fixtures.finishJobs = false;
  });
  await chooseBackup(page);
  await expect(dialog(page)).toContainText("Uploaded files are not included and will not be restored");
  await expect(dialog(page).getByRole("button", { name: "Choose recovery file", exact: true })).toHaveCount(0);
  await assertFits(page);
  await capture(page, "restore-database-only-720x560");
  await fillRestore(page, false);
  await replace(page).click();
  await expect(dialog(page)).toBeHidden();
  expect(await page.evaluate(() => window.careTest.calls.find((call) =>
    call.method === "RestoreFromFile")?.args[1])).toBe("");
  await expect(root(page)).not.toContainText("The backup was restored");
});

test("an encrypted companion archive still requires the backup recovery file", async ({ page }) => {
  await openBackups(page);
  await page.evaluate(() => {
    const preview = window.careTest;
    preview.fixtures.importedBackup = {
      ...preview.fixtures.importedBackup, encrypted: true,
      db_dump: "care-20260930-020000.dump",
      files_archive: "files-20260930-020000.tar.gz.enc",
      path: `${preview.fixtures.backupDir}/care-20260930-020000.dump`,
    };
  });
  await chooseBackup(page);
  await password(page).fill(samplePassword);
  await acknowledge(page).check();
  await expect(dialog(page).getByRole("button", { name: "Choose recovery file", exact: true })).toBeVisible();
  await expect(replace(page)).toBeDisabled();
  expect(await callCount(page, "RestoreFromFile")).toBe(0);
});

test("changed companion files require a fresh replacement acknowledgement", async ({ page }) => {
  await openBackups(page);
  await chooseBackup(page);
  await fillRestore(page);
  await page.evaluate(() => { window.careTest.fixtures.importedBackup.files_archive = ""; });
  await replace(page).click();
  await expect(dialog(page).getByRole("alert")).toContainText("The selected backup has changed");
  await expect(acknowledge(page)).not.toBeChecked();
  await expect(dialog(page)).toContainText("Uploaded files are not included");
  await expect(replace(page)).toBeDisabled();
  expect(await callCount(page, "RestoreFromFile")).toBe(0);
});

test("a backup that vanishes before confirmation is checked again", async ({ page }) => {
  await openBackups(page);
  await chooseBackup(page);
  await fillRestore(page);
  await page.evaluate(() => window.careTest.failNext("InspectBackupFile", "couldn't open that file: no such file"));
  await replace(page).click();
  await expect(dialog(page).getByRole("alert")).toContainText("The selected backup is no longer usable");
  await expect(password(page)).toHaveValue(samplePassword);
  expect(await callCount(page, "RestoreFromFile")).toBe(0);
});

test("native role rejection remains a rejection even with a valid local form", async ({ page }) => {
  await openBackups(page);
  await chooseBackup(page);
  await fillRestore(page);
  await page.evaluate(() => { window.careTest.state.role = "client"; });
  await replace(page).click();
  await expect(dialog(page).getByRole("alert")).toContainText("This clinic isn't ready for that");
  await expect(dialog(page)).toBeVisible();
  await expect(root(page)).not.toContainText("Restoring the selected backup");
  await expect(root(page)).not.toContainText("The backup was restored");
});

test("the log button cannot accidentally submit an acknowledged restore", async ({ page }) => {
  await openBackups(page);
  await chooseBackup(page);
  await fillRestore(page);
  await page.evaluate(() => window.careTest.failNext("RestoreFromFile", "something else is still running"));
  await replace(page).click();
  await expect(dialog(page).getByRole("alert")).toBeVisible();
  const before = await callCount(page, "RestoreFromFile");
  await dialog(page).getByRole("button", { name: "Open log file for support" }).click();
  expect(await callCount(page, "OpenLogFolder")).toBe(1);
  expect(await callCount(page, "RestoreFromFile")).toBe(before);
});

test("presentation preserves backup wall-clock dates and rejects invalid timestamps", () => {
  expect(backupDate("care-20260230-020000.dump.enc")).toBeNull();
  expect(backupDate("care-20261001-250000.dump.enc")).toBeNull();
  expect(backupDate("other-20261001-020000.dump.enc")).toBeNull();
  expect(backupDate("care-manual-20261001-020004.dump.enc")?.getHours()).toBe(2);
  expect(backupWhen(backupDate("care-20261231-235900.dump.enc"), new Date(2027, 0, 1, 12)))
    .toBe("Yesterday, 23:59");
  expect(backupWhen(backupDate("care-20251001-020000.dump"), new Date(2026, 9, 1, 12)))
    .toContain("2025");
  expect(backupWhen(null)).toBe("Date unavailable");
});

test("presentation groups and sorts manual and automatic backups together", () => {
  const groups = backupGroups([...boardBackups].reverse(), new Date(2026, 9, 1, 12));
  expect(groups.map(([name]) => name)).toEqual(["Today", "Yesterday", "Earlier"]);
  expect(groups[1][1].map(({ backup }) => backup.db_dump))
    .toEqual(["care-manual-20260930-114700.dump.enc", "care-20260930-020000.dump.enc"]);
});

test("presentation keeps native diagnostic detail out of every error message", () => {
  for (const context of ["folder", "backup", "inspect", "restore", "recovery", "storage"] as const) {
    const result = backupProblem("unexpected error: exit 42 /private/clinic\ninternal stack trace", context);
    expect(`${result.title} ${result.detail}`).not.toMatch(/exit 42|\/private\/clinic|stack trace/);
  }
  expect(backupProblem("the recovery file does not match this backup", "restore").title)
    .toBe("That recovery file doesn't match");
});

test("presentation derives capacity only from reported storage fields", () => {
  const space = {
    dir: "/Volumes/USB", free: 1000, total: 2000, need: 100, set_bytes: 50,
    days_left: 400, shares_docker_drive: false, level: "ok" as const, message: "USB hardware guess",
  };
  expect(backupSpaceSummary(space)).toBe("Room for about 1 year of backups");
  expect(backupSpaceSummary({ ...space, days_left: 728 })).toBe("Room for about 2 years of backups");
  expect(backupSpaceSummary({ ...space, free: 50 })).toBe("Not enough room for the next backup");
  expect(backupSpaceSummary({ ...space, level: "unknown", total: 0 }))
    .toBe("Free space couldn't be checked");
});
