import path from "node:path";
import { defineConfig, devices } from "@playwright/test";

// The browser acceptance runs through scripts/check-phase4-browser.sh, which
// supplies the proxy origin, fixture and a private 0700 artifact directory.
// Traces, video and automatic screenshots stay off so bearer credentials can
// never be captured; explicit screenshots are taken only after sign-in.
const artifacts =
  process.env.PHASE4_ARTIFACT_DIR ??
  path.join(process.cwd(), ".phase4-artifacts");

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: true,
  timeout: 240_000,
  globalTimeout: 540_000,
  expect: { timeout: 15_000 },
  reporter: [
    ["list"],
    ["json", { outputFile: path.join(artifacts, "report.json") }],
  ],
  outputDir: path.join(artifacts, "playwright-output"),
  use: {
    baseURL: process.env.PHASE4_BASE_URL ?? "http://127.0.0.1:18090",
    trace: "off",
    video: "off",
    screenshot: "off",
    actionTimeout: 15_000,
    navigationTimeout: 30_000,
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
