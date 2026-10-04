import { expect, test } from "@playwright/test";
import type {} from "./fixtures/host";

test.beforeEach(async ({ page, baseURL }) => {
  const origin = new URL(baseURL ?? "http://127.0.0.1:41783").origin.replace(/^http/, "ws");
  await page.routeWebSocket(`${origin}/**`, (socket) => socket.send('{"type":"connected"}'));
  await page.goto("/tests/fixtures/index.html?scenario=available");
});

test("in-place handoff never claims installation before exit or unlocks navigation", async ({ page }) => {
  await expect(page.getByText(/Updates this installed copy and reopens CARE Clinic/)).toBeVisible();
  await page.getByRole("button", { name: "Update now", exact: true }).click();
  for (const phase of ["verifying", "installing", "restarting"] as const) {
    await page.evaluate((next) => window.careTest.progress({ phase: next, done: 0, total: 0 }), phase);
    await expect(page.getByRole("button", { name: "Start setup", exact: true })).toBeDisabled();
  }
  await page.evaluate(() => window.careTest.finishUpdate());
  await expect(page.getByText("Restarting CARE Clinic to finish updating…", { exact: true })).toBeVisible();
  await expect(page.getByText(/Installed|Drag CARE Clinic|Follow it to finish/)).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Start setup", exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: /^(OK|Done|Dismiss)$/ })).toHaveCount(0);
});

test("disk-image and development copies get actionable installation guidance", async ({ page }) => {
  await page.getByRole("button", { name: "Update now", exact: true }).click();
  await page.evaluate(() => window.careTest.finishUpdate(
    "update location unavailable: internal private installation path",
  ));
  await expect(page.getByRole("alert")).toContainText("Open an installed copy to update");
  await expect(page.getByRole("alert")).toContainText("outside the disk image");
  await expect(page.getByText("internal private installation path")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Start setup", exact: true })).toBeEnabled();
});
