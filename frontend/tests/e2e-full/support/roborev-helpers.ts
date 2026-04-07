import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import type { Page } from "@playwright/test";
import { expect } from "@playwright/test";

function readEnvFile(): Record<string, string> {
  const envPath = process.env["ROBOREV_ENV_FILE"];
  if (!envPath) {
    throw new Error(
      "ROBOREV_ENV_FILE not set — run via scripts/run-roborev-e2e.sh",
    );
  }
  const content = readFileSync(envPath, "utf-8");
  const env: Record<string, string> = {};
  for (const line of content.split("\n")) {
    const eq = line.indexOf("=");
    if (eq > 0) {
      env[line.slice(0, eq)!] = line.slice(eq + 1);
    }
  }
  return env;
}

function composeExec(cmd: string): void {
  const env = readEnvFile();
  execSync(`docker compose ${cmd}`, {
    cwd: env["COMPOSE_DIR"],
    env: { ...process.env, ...env },
    stdio: "pipe",
    timeout: 30_000,
  });
}

export function stopDaemon(): void {
  composeExec("stop roborev");
}

export function startDaemon(): void {
  composeExec("start roborev");
  waitForDaemonHealthy();
}

export function restartDaemon(): void {
  composeExec("restart roborev");
  waitForDaemonHealthy();
}

function waitForDaemonHealthy(): void {
  const env = readEnvFile();
  const port = env["ROBOREV_PORT"] ?? "17373";
  const url = `http://127.0.0.1:${port}/api/status`;
  for (let i = 0; i < 30; i++) {
    try {
      execSync(`curl -sf ${url}`, {
        stdio: "pipe",
        timeout: 2_000,
      });
      return;
    } catch {
      execSync("sleep 1", { stdio: "pipe" });
    }
  }
  throw new Error("Daemon not healthy after 30 attempts");
}

export async function waitForReviewsReady(
  page: Page,
): Promise<void> {
  await page.goto("/reviews");
  await expect(
    page.locator(".job-table"),
  ).toBeVisible({ timeout: 15_000 });
}

export async function waitForJobRows(
  page: Page,
  min: number,
): Promise<void> {
  const rows = page.locator(".job-row");
  await expect(
    async () => {
      const count = await rows.count();
      expect(count).toBeGreaterThanOrEqual(min);
    },
  ).toPass({ timeout: 10_000 });
}

export async function openDrawer(
  page: Page,
  jobId: number,
): Promise<void> {
  await page.goto(`/reviews/${jobId}`);
  await expect(
    page.locator(".drawer"),
  ).toBeVisible({ timeout: 10_000 });
}
