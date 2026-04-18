import { spawn, type ChildProcess } from "node:child_process";
import { mkdtempSync, readFileSync } from "node:fs";
import { request as httpRequest } from "node:http";
import { request as httpsRequest } from "node:https";
import { cp, mkdir, readFile, rm, stat, writeFile } from "node:fs/promises";
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

export type IsolatedE2EServer = {
  info: E2EServerInfo;
  stop: () => Promise<void>;
};

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../../../..");
const serverInfoDir = mkdtempSync(path.join(os.tmpdir(), "middleman-e2e-"));
const serverInfoFile = path.join(serverInfoDir, "server-info.json");
const startupTimeoutMs = 60_000;
const pollIntervalMs = 100;
const reachabilityTimeoutMs = 1_000;
const ownedServerEnvVar = "PLAYWRIGHT_E2E_SERVER_OWNED";

type ManagedChildLike = {
  pid?: number | undefined;
  exitCode: number | null;
};

let serverPromise: Promise<E2EServerInfo> | null = null;
let managedChild: ChildProcess | null = null;
let cleanupInstalled = false;

async function pathExists(filePath: string): Promise<boolean> {
  try {
    await stat(filePath);
    return true;
  } catch {
    return false;
  }
}

async function waitForExit(child: ChildProcess, description: string): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    child.once("error", reject);
    child.once("exit", (code, signal) => {
      if (code === 0) {
        resolve();
        return;
      }
      reject(new Error(`${description} exited with code ${code ?? "null"} signal ${signal ?? "null"}`));
    });
  });
}

export async function ensureEmbeddedFrontend(rootDir: string = repoRoot): Promise<void> {
  const embeddedDist = path.join(rootDir, "internal", "web", "dist");
  const embeddedIndex = path.join(embeddedDist, "index.html");
  if (await pathExists(embeddedIndex)) {
    return;
  }

  const frontendDir = path.join(rootDir, "frontend");
  const frontendDist = path.join(frontendDir, "dist");
  const frontendIndex = path.join(frontendDist, "index.html");

  if (!(await pathExists(frontendIndex))) {
    const build = spawn("bun", ["run", "build"], {
      cwd: frontendDir,
      stdio: "inherit",
      env: process.env,
    });
    await waitForExit(build, "frontend build");
  }

  await rm(embeddedDist, { recursive: true, force: true });
  await mkdir(path.dirname(embeddedDist), { recursive: true });
  await cp(frontendDist, embeddedDist, { recursive: true });
  await writeFile(path.join(embeddedDist, "stub.html"), "ok\n");
}

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

function readServerInfoSync(filePath: string): E2EServerInfo | null {
  try {
    return JSON.parse(readFileSync(filePath, "utf8")) as E2EServerInfo;
  } catch {
    return null;
  }
}

async function isServerReachable(baseURL: string): Promise<boolean> {
  return await new Promise<boolean>((resolve) => {
    const url = new URL(baseURL);
    const request = (url.protocol === "https:" ? httpsRequest : httpRequest)(
      url,
      { method: "GET", timeout: reachabilityTimeoutMs },
      (response) => {
        response.resume();
        resolve(
          (response.statusCode ?? 0) >= 200 && (response.statusCode ?? 0) < 300,
        );
      },
    );

    request.on("error", () => {
      resolve(false);
    });
    request.on("timeout", () => {
      request.destroy();
      resolve(false);
    });
    request.end();
  });
}

export async function getReusableServerInfo(
  filePath: string,
): Promise<E2EServerInfo | null> {
  const info = await readServerInfo(filePath);
  if (!info) {
    return null;
  }
  if (!(await isServerReachable(info.base_url))) {
    return null;
  }
  return info;
}

export async function waitForServerInfo(
  filePath: string,
  child: Pick<ManagedChildLike, "exitCode">,
): Promise<E2EServerInfo> {
  const deadline = Date.now() + startupTimeoutMs;
  while (Date.now() < deadline) {
    const info = await readServerInfo(filePath);
    if (info && (await isServerReachable(info.base_url))) {
      return info;
    }
    if (child.exitCode !== null) {
      throw new Error(
        `e2e server exited with code ${child.exitCode} before becoming ready from ${filePath}`,
      );
    }
    await delay(pollIntervalMs);
  }
  throw new Error(`timed out waiting for ready e2e server from ${filePath}`);
}

async function removeServerInfo(filePath: string): Promise<void> {
  await rm(filePath, { force: true });
}

async function spawnServer(infoFile: string): Promise<{
  child: ChildProcess;
  info: E2EServerInfo;
}> {
  await ensureEmbeddedFrontend();

  const args = [
    "run",
    "./cmd/e2e-server",
    "-port",
    "0",
    "-server-info-file",
    infoFile,
  ];
  if (process.env.ROBOREV_ENDPOINT) {
    args.push("-roborev", process.env.ROBOREV_ENDPOINT);
  }

  const child = spawn("go", args, {
    cwd: repoRoot,
    stdio: "inherit",
    env: process.env,
  });

  return {
    child,
    info: await waitForServerInfo(infoFile, child),
  };
}

export function cleanupManagedServerProcess(
  child: ManagedChildLike | null = managedChild,
  infoFile: string | undefined = process.env.PLAYWRIGHT_E2E_SERVER_INFO_FILE,
): void {
  const serverPID = infoFile ? readServerInfoSync(infoFile)?.pid : undefined;
  const fallbackPID = child?.exitCode === null ? child.pid : undefined;
  const pid = serverPID ?? fallbackPID;
  if (!pid) {
    return;
  }

  try {
    process.kill(pid, "SIGTERM");
  } catch {
    // Process already exited.
  }
}

function installCleanup(infoFile: string): void {
  if (cleanupInstalled) {
    return;
  }
  cleanupInstalled = true;

  const cleanup = () => {
    cleanupManagedServerProcess(managedChild, infoFile);
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

async function startManagedServer(): Promise<E2EServerInfo> {
  const started = await spawnServer(serverInfoFile);
  managedChild = started.child;

  installCleanup(serverInfoFile);

  const info = started.info;
  process.env.PLAYWRIGHT_E2E_BASE_URL = info.base_url;
  process.env.PLAYWRIGHT_E2E_SERVER_INFO_FILE = serverInfoFile;
  process.env[ownedServerEnvVar] = "1";
  return info;
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
      const info = await getReusableServerInfo(existingInfoFile);
      if (info) {
        process.env.PLAYWRIGHT_E2E_BASE_URL = info.base_url;
        process.env.PLAYWRIGHT_E2E_SERVER_INFO_FILE = existingInfoFile;
        return info;
      }

      delete process.env.PLAYWRIGHT_E2E_BASE_URL;
      delete process.env.PLAYWRIGHT_E2E_SERVER_INFO_FILE;
      return await startManagedServer();
    })();
    return await serverPromise;
  }

  serverPromise = startManagedServer();
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

export async function startIsolatedE2EServer(): Promise<IsolatedE2EServer> {
  const isolatedInfoDir = mkdtempSync(path.join(os.tmpdir(), "middleman-e2e-"));
  const isolatedInfoFile = path.join(isolatedInfoDir, "server-info.json");
  const started = await spawnServer(isolatedInfoFile);

  return {
    info: started.info,
    stop: async () => {
      cleanupManagedServerProcess(started.child, isolatedInfoFile);
      await removeServerInfo(isolatedInfoFile);
      await rm(isolatedInfoDir, { force: true, recursive: true });
    },
  };
}
