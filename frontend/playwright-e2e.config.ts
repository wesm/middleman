import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./tests/e2e-full",
  testIgnore: /support\//,
  fullyParallel: true,
  timeout: 30_000,
  expect: {
    timeout: 5_000,
  },
  use: {
    baseURL: "http://127.0.0.1:4174",
    trace: "on-first-retry",
  },
  webServer: {
    command: process.env.ROBOREV_ENDPOINT
      ? `../cmd/e2e-server/e2e-server -port 4174 -roborev ${process.env.ROBOREV_ENDPOINT}`
      : "../cmd/e2e-server/e2e-server -port 4174",
    port: 4174,
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
  },
  projects: [
    {
      name: "chromium",
      testIgnore: /roborev/,
      use: {
        ...devices["Desktop Chrome"],
      },
    },
    {
      name: "roborev",
      testMatch: /roborev/,
      fullyParallel: false,
      workers: 1,
      use: {
        ...devices["Desktop Chrome"],
      },
    },
  ],
});
