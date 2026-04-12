import { mkdir, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

const LOCK_ROOT = path.join(os.tmpdir(), "middleman-playwright-locks");

async function sleep(ms: number): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, ms));
}

export async function acquireExclusiveLock(
  name: string,
): Promise<() => Promise<void>> {
  await mkdir(LOCK_ROOT, { recursive: true });
  const lockDir = path.join(LOCK_ROOT, name);

  for (;;) {
    try {
      await mkdir(lockDir);
      return async () => {
        await rm(lockDir, { recursive: true, force: true });
      };
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "EEXIST") {
        throw error;
      }
      await sleep(100);
    }
  }
}
