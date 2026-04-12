import { afterEach, describe, expect, it, vi } from "vitest";
import { mkdtempSync, writeFileSync } from "node:fs";
import { createServer } from "node:http";
import { readFile, stat } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import process from "node:process";
import * as e2eServerModule from "../../../tests/e2e-full/support/e2eServer";

const { stopE2EServer } = e2eServerModule;

const originalEnv = { ...process.env };

async function fileExists(filePath: string): Promise<boolean> {
  try {
    await stat(filePath);
    return true;
  } catch {
    return false;
  }
}

afterEach(() => {
  vi.restoreAllMocks();
  process.env = { ...originalEnv };
});

describe("waitForServerInfo", () => {
  it("waits until the reported base URL accepts connections", async () => {
    const waitForServerInfo = (
      e2eServerModule as {
        waitForServerInfo?: (
          filePath: string,
          child: { exitCode: number | null },
        ) => Promise<{
          host: string;
          port: number;
          base_url: string;
          pid: number;
        }>;
      }
    ).waitForServerInfo;

    expect(waitForServerInfo).toBeTypeOf("function");
    if (!waitForServerInfo) {
      return;
    }

    const dir = mkdtempSync(path.join(os.tmpdir(), "e2e-server-test-"));
    const infoFile = path.join(dir, "server-info.json");
    const readyAt = Date.now() + 150;
    const server = createServer((_req, res) => {
      if (Date.now() < readyAt) {
        res.writeHead(503, { "content-type": "text/plain" });
        res.end("not ready");
        return;
      }

      res.writeHead(200, { "content-type": "text/plain" });
      res.end("ok");
    });

    const port = await new Promise<number>((resolve, reject) => {
      server.listen(0, "127.0.0.1", () => {
        const address = server.address();
        if (!address || typeof address === "string") {
          reject(new Error("server did not bind a TCP port"));
          return;
        }
        resolve(address.port);
      });
    });

    writeFileSync(
      infoFile,
      JSON.stringify({
        host: "127.0.0.1",
        port,
        base_url: `http://127.0.0.1:${port}`,
        pid: 99999,
      }),
    );

    const startedAt = Date.now();
    const info = await waitForServerInfo(infoFile, { exitCode: null });

    expect(info.base_url).toBe(`http://127.0.0.1:${port}`);
    expect(Date.now() - startedAt).toBeGreaterThanOrEqual(100);

    await new Promise<void>((resolve, reject) => {
      server.close((error) => {
        if (error) {
          reject(error);
          return;
        }
        resolve();
      });
    });
  });
});

describe("cleanupManagedServerProcess", () => {
  it("kills the real server pid from server-info instead of the wrapper pid", () => {
    const cleanupManagedServerProcess = (
      e2eServerModule as {
        cleanupManagedServerProcess?: (
          managedChild?: { pid?: number; exitCode: number | null } | null,
        ) => void;
      }
    ).cleanupManagedServerProcess;

    expect(cleanupManagedServerProcess).toBeTypeOf("function");
    if (!cleanupManagedServerProcess) {
      return;
    }

    const dir = mkdtempSync(path.join(os.tmpdir(), "e2e-server-test-"));
    const infoFile = path.join(dir, "server-info.json");
    writeFileSync(
      infoFile,
      JSON.stringify({
        host: "127.0.0.1",
        port: 1234,
        base_url: "http://127.0.0.1:1234",
        pid: 99999,
      }),
    );
    process.env.PLAYWRIGHT_E2E_SERVER_INFO_FILE = infoFile;

    const killSpy = vi.spyOn(process, "kill").mockImplementation(() => true);

    cleanupManagedServerProcess({ pid: 11111, exitCode: null });

    expect(killSpy).toHaveBeenCalledWith(99999, "SIGTERM");
    expect(killSpy).not.toHaveBeenCalledWith(11111, "SIGTERM");
  });
});

describe("stopE2EServer", () => {
  it("does not kill or delete externally managed server resources", async () => {
    const dir = mkdtempSync(path.join(os.tmpdir(), "e2e-server-test-"));
    const infoFile = path.join(dir, "server-info.json");
    const siblingFile = path.join(dir, "keep.txt");
    writeFileSync(
      infoFile,
      JSON.stringify({
        host: "127.0.0.1",
        port: 1234,
        base_url: "http://127.0.0.1:1234",
        pid: 99999,
      }),
    );
    writeFileSync(siblingFile, "keep");

    process.env.PLAYWRIGHT_E2E_SERVER_INFO_FILE = infoFile;
    process.env.PLAYWRIGHT_E2E_BASE_URL = "http://127.0.0.1:1234";
    delete process.env.PLAYWRIGHT_E2E_SERVER_OWNED;

    const killSpy = vi.spyOn(process, "kill").mockImplementation(() => true);

    await stopE2EServer();

    expect(killSpy).not.toHaveBeenCalled();
    expect(await fileExists(infoFile)).toBe(true);
    expect(await readFile(siblingFile, "utf8")).toBe("keep");
  });

  it("only tears down resources created by this helper", async () => {
    const dir = mkdtempSync(path.join(os.tmpdir(), "e2e-server-test-"));
    const infoFile = path.join(dir, "server-info.json");
    const siblingFile = path.join(dir, "keep.txt");
    writeFileSync(
      infoFile,
      JSON.stringify({
        host: "127.0.0.1",
        port: 1234,
        base_url: "http://127.0.0.1:1234",
        pid: 99999,
      }),
    );
    writeFileSync(siblingFile, "keep");

    process.env.PLAYWRIGHT_E2E_SERVER_INFO_FILE = infoFile;
    process.env.PLAYWRIGHT_E2E_BASE_URL = "http://127.0.0.1:1234";
    process.env.PLAYWRIGHT_E2E_SERVER_OWNED = "1";

    const killSpy = vi.spyOn(process, "kill").mockImplementation(() => true);

    await stopE2EServer();

    expect(killSpy).toHaveBeenCalledWith(99999, "SIGTERM");
    expect(await fileExists(infoFile)).toBe(false);
    expect(await readFile(siblingFile, "utf8")).toBe("keep");
    expect(process.env.PLAYWRIGHT_E2E_SERVER_OWNED).toBeUndefined();
    expect(process.env.PLAYWRIGHT_E2E_SERVER_INFO_FILE).toBeUndefined();
    expect(process.env.PLAYWRIGHT_E2E_BASE_URL).toBeUndefined();
  });
});
