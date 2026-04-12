import { afterEach, describe, expect, it, vi } from "vitest";
import { mkdtempSync, writeFileSync } from "node:fs";
import { readFile, stat } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import process from "node:process";
import { stopE2EServer } from "../../../tests/e2e-full/support/e2eServer";

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
