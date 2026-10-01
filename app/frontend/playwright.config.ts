import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  fullyParallel: true,
  workers: 2,
  reporter: "list",
  outputDir: "./test-results",
  use: {
    baseURL: "http://127.0.0.1:41783",
    viewport: { width: 1100, height: 700 },
    trace: "retain-on-failure",
  },
  webServer: {
    command: "npm run dev -- --mode test --host 127.0.0.1 --port 41783 --strictPort",
    url: "http://127.0.0.1:41783/tests/fixtures/index.html",
    reuseExistingServer: false,
  },
});
