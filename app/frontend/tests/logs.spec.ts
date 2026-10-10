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

test("logs page reports collection status", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?scenario=panel-current");
  await page.getByRole("button", { name: "Logs", exact: true }).click();
  const logs = page.locator('.panel-page[aria-label="Logs"]');
  await expect(logs.getByRole("status", { name: "Engine log collection" })).toContainText("Collecting");
  await expect(logs.getByText("CARE engine update required")).toHaveCount(0);
});

test("an outdated engine is updated automatically and the clinic isn't shown as running meanwhile", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?scenario=panel-engine-outdated");
  const status = page.getByRole("region", { name: "Clinic status" });
  await expect(status.getByRole("heading", { name: "Updating" })).toBeVisible();
  await expect(status.getByText(/CARE engine is being updated/)).toBeVisible();
  await expect(status.getByRole("heading", { name: "Running" })).toHaveCount(0);
  await expect(status.getByRole("heading", { name: "Running" })).toBeVisible();
  await page.getByRole("button", { name: "Logs", exact: true }).click();
  const logs = page.locator('.panel-page[aria-label="Logs"]');
  await expect(logs.getByRole("status", { name: "Engine log collection" })).toContainText("Collecting");
  await expect(page.getByText("CARE engine update required")).toHaveCount(0);
});

test("a failed automatic engine update offers a retry", async ({ page }) => {
  await page.goto("/tests/fixtures/index.html?scenario=panel-engine-update-failed");
  const overview = page.locator('.panel-page[aria-label="Overview"]');
  await expect(overview.getByText("CARE engine update required", { exact: true })).toBeVisible();
  await expect(overview.getByText(/needs CARE engine 0\.1\.1, but 0\.1\.0 is running/)).toBeVisible();
  await page.getByRole("button", { name: "Logs", exact: true }).click();
  const logs = page.locator('.panel-page[aria-label="Logs"]');
  await expect(logs.getByRole("status", { name: "Engine log collection" })).toContainText("Engine update required");
  await logs.getByRole("button", { name: "Update CARE engine", exact: true }).click();
  await expect(logs.getByRole("status", { name: "Engine log collection" })).toContainText("Collecting");
  await expect(page.getByText("CARE engine update required")).toHaveCount(0);
});
