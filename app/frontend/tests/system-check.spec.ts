import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { expect, test, type Page } from "@playwright/test";

import type {} from "./fixtures/host";

let browserErrors: string[] = [];
test.beforeEach(async ({ page, baseURL }) => {
  const socketOrigin = new URL(baseURL ?? "http://127.0.0.1:41783").origin.replace(/^http/, "ws");
  await page.routeWebSocket(`${socketOrigin}/**`, (socket) => socket.send('{"type":"connected"}'));
  browserErrors = [];
  page.on("pageerror", (error) => browserErrors.push(error.message));
});
test.afterEach(() => {
  expect(browserErrors).toEqual([]);
});

async function screenshot(page: Page, name: string) {
  const directory = process.env.CARE_SCREENSHOTS_DIR;
  if (!directory) return;
  await mkdir(directory, { recursive: true });
  await page.evaluate(() => document.fonts.ready);
  await page.screenshot({ path: join(directory, `${name}.png`), animations: "disabled" });
}

const heading = (page: Page) => page.getByRole("heading", { name: "System requirements" });
const welcome = (page: Page) => page.getByRole("heading", { name: "Set up CARE on this computer" });
const rows = (page: Page) => page.getByRole("list", { name: "Requirements" }).getByRole("listitem");

test("a supported computer shows every requirement as met and continues to the start screen", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?systemCheck=1");
  await expect(heading(page)).toBeVisible();
  await expect(page.getByText("This computer meets CARE's requirements")).toBeVisible();
  await expect(rows(page)).toHaveCount(5);
  for (const label of ["Operating system", "Processor", "Memory (RAM)", "Free storage", "Virtualization"]) {
    await expect(rows(page).filter({ hasText: label })).toContainText("Meets");
  }
  await expect(rows(page).filter({ hasText: "Memory (RAM)" })).toContainText("16 GB");
  await screenshot(page, "system-check-ok");

  await page.getByRole("button", { name: "Continue", exact: true }).click();
  await expect(welcome(page)).toBeVisible();
  expect(await page.evaluate(() => sessionStorage.getItem("care.systemCheckReviewed"))).toBe("1");
});

test("Windows 10 and low memory warn but never block", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?systemCheck=1&scenario=system-windows10");
  await expect(page.getByText("This computer doesn't meet all of CARE's requirements")).toBeVisible();
  await expect(page.getByText("CARE may not work as expected.", { exact: false })).toBeVisible();
  const os = rows(page).filter({ hasText: "Operating system" });
  await expect(os).toContainText("Windows 10 Pro");
  await expect(os).toContainText("Below requirement");
  await expect(os).toContainText("doesn't support Windows 10");
  await expect(rows(page).filter({ hasText: "Memory (RAM)" })).toContainText("Below requirement");
  await expect(rows(page).filter({ hasText: "Virtualization" })).toContainText("Not checked");
  await screenshot(page, "system-check-warn");

  const next = page.getByRole("button", { name: "Continue anyway", exact: true });
  await expect(next).toBeEnabled();
  await next.click();
  await expect(welcome(page)).toBeVisible();
});

test("a failed check can be retried and still lets the operator continue", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?systemCheck=1");
  await expect(heading(page)).toBeVisible();
  await page.evaluate(() => window.careTest.failNext("SystemRequirements", "sysctl unavailable"));
  await page.getByRole("button", { name: "Check again", exact: true }).click();
  await expect(page.getByText("Couldn't check this computer")).toBeVisible();
  await expect(page.getByRole("button", { name: "Continue anyway", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: "Check again", exact: true }).click();
  await expect(page.getByText("This computer meets CARE's requirements")).toBeVisible();
});

test("Linux skips the system check", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?systemCheck=1&platform=linux");
  await expect(welcome(page)).toBeVisible();
  expect(await page.evaluate(() => window.careTest.calls.some((call) => call.method === "SystemRequirements"))).toBe(false);
});

test("a computer that already has a role never sees the check", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?systemCheck=1&scenario=setup-space");
  await expect(heading(page)).toHaveCount(0);
});
