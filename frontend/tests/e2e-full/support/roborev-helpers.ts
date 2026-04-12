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

// Sentinel-bounded probe of the daemon backing the e2e server.
// The seed creates ~73 jobs total; a real local roborev daemon will
// have orders of magnitude more. If the e2e server proxies to an
// unmanaged daemon (e.g. tests run directly without
// scripts/run-roborev-e2e.sh while a local daemon is bound to
// 127.0.0.1:7373 — the e2e server's silent default), individual
// tests would fail with mysterious data mismatches. Refuse upfront
// with a clear pointer at the runner script instead.
export async function assertSeededRoborevDaemon(): Promise<void> {
  const baseURL = process.env["PLAYWRIGHT_E2E_BASE_URL"];
  if (!baseURL) {
    throw new Error(
      "PLAYWRIGHT_E2E_BASE_URL is not set. Run roborev e2e tests " +
        "via scripts/run-roborev-e2e.sh, not playwright directly.",
    );
  }
  const url = `${baseURL}/api/roborev/api/status`;
  let body: Record<string, unknown>;
  try {
    const res = await fetch(url, {
      signal: AbortSignal.timeout(3_000),
    });
    if (!res.ok) {
      throw new Error(`HTTP ${res.status}`);
    }
    body = (await res.json()) as Record<string, unknown>;
  } catch (err) {
    throw new Error(
      `roborev daemon at ${url} is not reachable (${String(err)}). ` +
        "Run roborev e2e tests via scripts/run-roborev-e2e.sh.",
      { cause: err },
    );
  }
  const num = (key: string): number => {
    const v = body[key];
    return typeof v === "number" ? v : 0;
  };
  const total =
    num("queued_jobs") +
    num("running_jobs") +
    num("completed_jobs") +
    num("failed_jobs") +
    num("canceled_jobs") +
    num("applied_jobs") +
    num("rebased_jobs");
  // Seed creates 73 jobs. Allow headroom for jobs created during the
  // run (the rerun-action test enqueues a new job).
  const maxSeededJobs = 200;
  if (total > maxSeededJobs) {
    throw new Error(
      `roborev daemon at ${url} is not the script-seeded test ` +
        `daemon (total_jobs=${total}, expected <= ${maxSeededJobs}). ` +
        "The middleman e2e server defaults the roborev endpoint to " +
        "http://127.0.0.1:7373 when ROBOREV_ENDPOINT is unset, so " +
        "running playwright directly while a real local roborev " +
        "daemon is running will silently hit it. Run roborev e2e " +
        "tests via scripts/run-roborev-e2e.sh instead.",
    );
  }
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
