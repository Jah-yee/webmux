import { defineConfig } from "@playwright/test";
import base from "./playwright.config";

// Reuse E2E's isolated production build, then measure on a dedicated worker
// without other tests competing for the same CPU.
export default defineConfig({
  ...base,
  testDir: "./test/performance",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  outputDir: "test-results/performance",
  reporter: [
    [process.env.CI ? "github" : "list"],
    ["html", { outputFolder: "playwright-report/performance", open: "never" }],
  ],
});
