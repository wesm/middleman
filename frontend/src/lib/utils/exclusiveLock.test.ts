// @vitest-environment node

import { mkdtemp, rm, stat } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import {
  acquireExclusiveLock,
  exclusiveLockPath,
} from "../../../tests/e2e-full/support/exclusiveLock";

async function delay(ms: number): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, ms));
}

describe("exclusive e2e lock", () => {
  const releaseFns: Array<() => Promise<void>> = [];
  const tempRoots: string[] = [];

  afterEach(async () => {
    while (releaseFns.length > 0) {
      await releaseFns.pop()?.();
    }
    while (tempRoots.length > 0) {
      await rm(tempRoots.pop() ?? "", { force: true, recursive: true });
    }
  });

  async function tempRoot(): Promise<string> {
    const root = await mkdtemp(path.join(os.tmpdir(), "middleman-lock-test-"));
    tempRoots.push(root);
    return root;
  }

  it("creates a stable lock file in the configured temp root", async () => {
    const root = await tempRoot();

    releaseFns.push(await acquireExclusiveLock("workspace-tmux", {
      rootDir: root,
    }));

    const info = await stat(exclusiveLockPath("workspace-tmux", root));

    expect(info.isFile()).toBe(true);
  });

  it("waits for an existing lock file before acquiring", async () => {
    const root = await tempRoot();
    const firstRelease = await acquireExclusiveLock("workspace-tmux", {
      rootDir: root,
    });
    releaseFns.push(firstRelease);

    let secondAcquired = false;
    const secondReleasePromise = acquireExclusiveLock("workspace-tmux", {
      rootDir: root,
    }).then((release) => {
      secondAcquired = true;
      return release;
    });

    await delay(25);
    expect(secondAcquired).toBe(false);

    await firstRelease();
    releaseFns.pop();

    const secondRelease = await secondReleasePromise;
    releaseFns.push(secondRelease);
    expect(secondAcquired).toBe(true);
  });
});
