import { expect, type Page } from "@playwright/test";
import type {} from "../fixtures/host";

export async function continueServerChecks(page: Page, { removeResidue = false } = {}) {
  await expect(page.locator("#setup-title")).toHaveText("Room for the clinic");
  const platform = await page.evaluate(() => window.careTest.state.platform);
  const titles = [
    "Room for the clinic",
    ...(platform === "windows" ? ["Getting Windows ready"] : []),
    "Installing what CARE needs",
    "Removing stale files from an earlier setup",
    ...(platform === "windows" ? ["Setting this network to Private"] : []),
  ];
  const next = page.getByRole("button", { name: "Continue", exact: true });
  for (const title of titles) {
    await expect(page.locator("#setup-title")).toHaveText(title);
    if (removeResidue && title === "Removing stale files from an earlier setup") {
      await expect(next).toBeDisabled();
      await page.getByRole("button", { name: "Remove it all", exact: true }).click();
    }
    await expect(next).toBeEnabled();
    await expect(page.locator("#setup-title")).toHaveText(title);
    await next.click();
  }
  await expect(page.locator("#setup-title")).toHaveText("Choosing the clinic address");
}
