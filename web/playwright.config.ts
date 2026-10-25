import { defineConfig } from "@playwright/test";
const runId = new Date()
  .toISOString()
  .replaceAll(":", "-")
  .replaceAll(".", "-");
export default defineConfig({
  testDir: "./e2e",
  testMatch: "*.spec.ts",
  workers: 4,
  outputDir: `./test-results/runs/${runId}`,
  use: {
    baseURL: process.env.GOEPAY_WEB_URL || "http://127.0.0.1:5173",
    trace: "retain-on-failure",
  },
  webServer: process.env.GOEPAY_WEB_URL
    ? undefined
    : {
        command: "npm run dev -- --port 5173",
        url: "http://127.0.0.1:5173",
        reuseExistingServer: true,
      },
  reporter: [
    ["list"],
    [
      "html",
      { outputFolder: `playwright-report/runs/${runId}`, open: "never" },
    ],
  ],
});
