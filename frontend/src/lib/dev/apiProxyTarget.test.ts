// @vitest-environment node

import { mkdirSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import {
  defaultDevApiUrl,
  resolveDevApiUrl,
} from "./apiProxyTarget";

describe("resolveDevApiUrl", () => {
  const tempDirs: string[] = [];

  afterEach(() => {
    tempDirs.length = 0;
  });

  it("prefers MIDDLEMAN_API_URL when present", () => {
    expect(
      resolveDevApiUrl({
        HOME: "/ignored",
        MIDDLEMAN_API_URL: "http://127.0.0.1:9123/custom",
      }),
    ).toBe("http://127.0.0.1:9123/custom");
  });

  it("reads host, port, and base path from MIDDLEMAN_HOME config", () => {
    const middlemanHome = makeTempDir();
    writeConfig(
      middlemanHome,
      `
host = "127.0.0.1"
port = 9123
base_path = "/middleman/"
`,
    );

    expect(
      resolveDevApiUrl({
        HOME: "/ignored",
        MIDDLEMAN_HOME: middlemanHome,
      }),
    ).toBe("http://127.0.0.1:9123/middleman");
  });

  it("falls back to the default config path under HOME", () => {
    const home = makeTempDir();
    writeConfig(
      path.join(home, ".config", "middleman"),
      `
port = 9234
`,
    );

    expect(
      resolveDevApiUrl({
        HOME: home,
      }),
    ).toBe("http://127.0.0.1:9234");
  });

  it("falls back to the default URL when config cannot be read", () => {
    expect(
      resolveDevApiUrl({
        HOME: "/missing-home",
      }),
    ).toBe(defaultDevApiUrl);
  });

  it("formats IPv6 loopback hosts correctly", () => {
    const middlemanHome = makeTempDir();
    writeConfig(
      middlemanHome,
      `
host = "::1"
port = 9345
`,
    );

    expect(
      resolveDevApiUrl({
        HOME: "/ignored",
        MIDDLEMAN_HOME: middlemanHome,
      }),
    ).toBe("http://[::1]:9345");
  });

  function makeTempDir(): string {
    const dir = path.join(
      os.tmpdir(),
      `middleman-api-proxy-target-${Date.now()}-${Math.random().toString(16).slice(2)}`,
    );
    mkdirSync(dir, { recursive: true });
    tempDirs.push(dir);
    return dir;
  }

  function writeConfig(baseDir: string, content: string): void {
    mkdirSync(baseDir, { recursive: true });
    writeFileSync(path.join(baseDir, "config.toml"), content.trimStart(), "utf8");
  }
});
