import { spawn, type ChildProcess } from "node:child_process";
import { mkdtempSync } from "node:fs";
import { readFile, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

export type E2EServerInfo = {
  host: string;
  port: number;
  base_url: string;
  pid: number;
};

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../../../..");
const serverInfoDir = mkdtempSync(path.join(os.tmpdir(), "middleman-e2e-"));
const serverInfoFile = path.join(serverInfoDir, "server-info.json");
const startupTimeoutMs = 30_000;
const pollIntervalMs = 100;
const ownedServerEnvVar = "PLAYWRIGHT_E2E_SERVER_OWNED";

let serverPromise: Promise<E2EServerInfo> | null = null;
let managedChild: ChildProcess | null = null;
let cleanupInstalled = false;

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function readServerInfo(filePath: string): Promise<E2EServerInfo | null> {
  try {
    return JSON.parse(await readFile(filePath, "utf8")) as E2EServerInfo;
  } catch {
    return null;
  }
}

async function waitForServerInfo(
  filePath: string,
  child: ChildProcess,
): Promise<E2EServerInfo> {
  const deadline = Date.now() + startupTimeoutMs;
  while (Date.now() < deadline) {
    const info = await readServerInfo(filePath);
    if (info) {
      return info;
    }
    if (child.exitCode !== null) {
      throw new Error(
        `e2e server exited with code ${child.exitCode} before writing ${filePath}`,
      );
    }
    await delay(pollIntervalMs);
  }
  throw new Error(`timed out waiting for e2e server info file ${filePath}`);
}

async function removeServerInfo(filePath: string): Promise<void> {
  await rm(filePath, { force: true });
}

function installCleanup(): void {
  if (cleanupInstalled) {
    return;
  }
  cleanupInstalled = true;

  const cleanup = () => {
    if (managedChild?.pid && managedChild.exitCode === null) {
      try {
        process.kill(managedChild.pid, "SIGTERM");
      } catch {
        // Process already exited.
      }
    }
  };

  process.once("exit", cleanup);
  process.once("SIGINT", () => {
    cleanup();
    process.exit(130);
  });
  process.once("SIGTERM", () => {
    cleanup();
    process.exit(143);
  });
}

export async function ensureE2EServer(): Promise<E2EServerInfo> {
  if (serverPromise) {
    return await serverPromise;
  }

  const existingBaseURL = process.env.PLAYWRIGHT_E2E_BASE_URL;
  const existingInfoFile = process.env.PLAYWRIGHT_E2E_SERVER_INFO_FILE;
  if (existingBaseURL && existingInfoFile) {
    delete process.env[ownedServerEnvVar];
    serverPromise = (async () => {
      const info = await readServerInfo(existingInfoFile);
      if (!info) {
        throw new Error(
          `failed to read existing e2e server info file ${existingInfoFile}`,
        );
      }
      return info;
    })();
    return await serverPromise;
  }

  serverPromise = (async () => {
    const args = [
      "run",
      "./cmd/e2e-server",
      "-port",
      "0",
      "-server-info-file",
      serverInfoFile,
    ];
    if (process.env.ROBOREV_ENDPOINT) {
      args.push("-roborev", process.env.ROBOREV_ENDPOINT);
    }

    managedChild = spawn("go", args, {
      cwd: repoRoot,
      stdio: "inherit",
      env: process.env,
    });

    installCleanup();

    const info = await waitForServerInfo(serverInfoFile, managedChild);
    process.env.PLAYWRIGHT_E2E_BASE_URL = info.base_url;
    process.env.PLAYWRIGHT_E2E_SERVER_INFO_FILE = serverInfoFile;
    process.env[ownedServerEnvVar] = "1";
    return info;
  })();

  return await serverPromise;
}

export async function stopE2EServer(): Promise<void> {
  const filePath = process.env.PLAYWRIGHT_E2E_SERVER_INFO_FILE;
  if (!filePath) {
    return;
  }
  if (process.env[ownedServerEnvVar] !== "1") {
    return;
  }

  const info = await readServerInfo(filePath);
  if (info?.pid) {
    try {
      process.kill(info.pid, "SIGTERM");
    } catch {
      // Process already exited.
    }
  }

  await removeServerInfo(filePath);
  delete process.env[ownedServerEnvVar];
  delete process.env.PLAYWRIGHT_E2E_SERVER_INFO_FILE;
  delete process.env.PLAYWRIGHT_E2E_BASE_URL;
}
