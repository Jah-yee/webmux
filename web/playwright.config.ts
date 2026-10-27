import { availableParallelism } from "node:os";
import { defineConfig, devices } from "@playwright/test";

export function resolveE2EWorkers(
  env: NodeJS.ProcessEnv = process.env,
  platform: NodeJS.Platform = process.platform,
  parallelism = availableParallelism(),
): number {
  const override = env.WEBMUX_E2E_WORKERS;
  if (override) {
    const workers = Number(override);
    if (!Number.isInteger(workers) || workers < 1)
      throw new Error("WEBMUX_E2E_WORKERS must be a positive integer");
    return workers;
  }
  if (env.CI) return platform === "darwin" ? 4 : 2;
  return Math.min(2, parallelism);
}

export default defineConfig({
  testDir: "./test/e2e",
  testMatch: "**/*.spec.ts",
  globalSetup: "./test/e2e/global-setup.mjs",
  // Test-scoped cleanup isolates each case; distribute long files across
  // workers and shards instead of leaving a worker idle at a file boundary.
  fullyParallel: true,
  // Each worker fixture owns its backend and sessions, so workers never
  // share state or reuse dev servers. Keep Ubuntu's two slots and macOS's
  // four to overlap PTY/Git and browser waits. Local runs use up to two to
  // cut the serial wall clock; WEBMUX_E2E_WORKERS overrides everywhere.
  workers: resolveE2EWorkers(),
  forbidOnly: Boolean(process.env.CI),
  failOnFlakyTests: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  timeout: 45_000,
  expect: { timeout: 10_000 },
  outputDir: "test-results/e2e",
  reporter: [
    [process.env.CI ? "github" : "list"],
    ["html", { outputFolder: "playwright-report/e2e", open: "never" }],
  ],
  use: {
    headless: true,
    screenshot: "only-on-failure",
    // Recording every test costs ~14% of suite time. CI retries once, so it
    // records only the retry; local runs never retry and keep failed traces.
    trace: process.env.CI ? "on-first-retry" : "retain-on-failure",
    video: process.env.CI ? "on-first-retry" : "retain-on-failure",
    // Real xterm DOM rendering makes output assertions independent of canvas
    // OCR. WebGL rendering remains covered by the Vitest browser suite.
    launchOptions: { args: ["--disable-webgl"] },
  },
  projects: [
    {
      name: "chromium-desktop",
      use: { ...devices["Desktop Chrome"], browserName: "chromium" },
    },
    {
      name: "chromium-mobile",
      use: { ...devices["Pixel 7"], browserName: "chromium" },
    },
  ],
});
