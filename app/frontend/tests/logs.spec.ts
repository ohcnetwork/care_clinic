import { expect, test } from "@playwright/test";
import type {} from "./fixtures/host";

test("logs page exposes shared folder, export and privacy guidance", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?scenario=panel-current");
  await page.getByRole("button", { name: "Logs", exact: true }).click();
  const logs = page.locator('.panel-page[aria-label="Logs"]');
  await expect(logs.getByRole("heading", { name: "Logs", exact: true })).toBeVisible();
  await expect(logs.getByText("guest-system.log", { exact: true })).toBeVisible();
  await logs.getByRole("button", { name: "Open logs folder", exact: true }).click();
  await logs.getByRole("button", { name: "Export diagnostics", exact: true }).click();
  await expect(logs.getByText("Review diagnostics before sharing", { exact: true })).toBeVisible();
});
