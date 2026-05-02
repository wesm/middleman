import { spawn, type ChildProcess } from "node:child_process";
import { chmod, lstat, mkdir } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import process from "node:process";

const LOCK_ROOT = path.join(
  os.tmpdir(),
  `middleman-playwright-locks-${typeof os.userInfo().uid === "number" ? os.userInfo().uid : "user"}`,
);
export type ExclusiveLockOptions = {
  rootDir?: string;
};

function safeLockName(name: string): string {
  const trimmed = name.trim();
  if (trimmed === "") {
    throw new Error("lock name must not be empty");
  }
  return trimmed.replace(/[^A-Za-z0-9._-]/g, "-");
}

export function exclusiveLockPath(
  name: string,
  rootDir: string = LOCK_ROOT,
): string {
  return path.join(rootDir, `${safeLockName(name)}.lock`);
}

function lockMetadata(): string {
  return JSON.stringify({
    created_at: new Date().toISOString(),
    pid: process.pid,
  }) + "\n";
}

function lockWorkerScript(): string {
  return `
    import { mkdir, rm, writeFile } from "node:fs/promises";
    import { rmSync } from "node:fs";
    import path from "node:path";

    const args = process.argv[1] === "[eval]"
      ? process.argv.slice(2)
      : process.argv.slice(1);
    const [lockDir, metadata] = args;
    const waitMs = 100;
    let acquired = false;
    let releasing = false;

    if (!lockDir || metadata === undefined) {
      throw new Error("usage: lock-worker <lock-dir> <metadata>");
    }

    const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

    async function acquire() {
      for (;;) {
        try {
          await mkdir(lockDir);
          acquired = true;
          await writeFile(path.join(lockDir, "metadata.json"), metadata);
          process.stdout.write("locked\\n");
          return;
        } catch (error) {
          if (error?.code !== "EEXIST") {
            throw error;
          }
          await delay(waitMs);
        }
      }
    }

    async function release() {
      if (releasing || !acquired) {
        return;
      }
      releasing = true;
      await rm(lockDir, { recursive: true, force: true });
      acquired = false;
    }

    process.stdin.resume();
    process.stdin.on("end", async () => {
      await release();
      process.exit(0);
    });
    for (const signal of ["SIGINT", "SIGTERM"]) {
      process.on(signal, async () => {
        await release();
        process.exit(0);
      });
    }
    process.on("exit", () => {
      if (acquired && !releasing) {
        try {
          rmSync(lockDir, { recursive: true, force: true });
        } catch {
          // Best-effort cleanup during process exit.
        }
      }
    });

    await acquire();
  `;
}

async function ensureLockRoot(rootDir: string): Promise<void> {
  try {
    const info = await lstat(rootDir);
    if (!info.isDirectory() || info.isSymbolicLink()) {
      throw new Error(`lock root is not a safe directory: ${rootDir}`);
    }
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") {
      throw error;
    }
    await mkdir(rootDir, { recursive: true, mode: 0o700 });
  }

  const info = await lstat(rootDir);
  if (!info.isDirectory() || info.isSymbolicLink()) {
    throw new Error(`lock root is not a safe directory: ${rootDir}`);
  }
  if (typeof process.getuid === "function" && info.uid !== process.getuid()) {
    throw new Error(`lock root is owned by uid ${info.uid}, expected ${process.getuid()}`);
  }
  await chmod(rootDir, 0o700);
}

function spawnLockProcess(lockPath: string): ChildProcess {
  return spawn(
    process.execPath,
    [
      "--input-type=module",
      "-e",
      lockWorkerScript(),
      lockPath,
      lockMetadata(),
    ],
    {
      stdio: ["pipe", "pipe", "pipe"],
    },
  );
}

async function waitForLockProcess(child: ChildProcess): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    let locked = false;
    let stderr = "";

    const cleanup = () => {
      child.stdout?.off("data", onStdout);
      child.stderr?.off("data", onStderr);
      child.off("error", onError);
      child.off("exit", onExit);
    };
    const onStdout = (chunk: Buffer) => {
      if (chunk.toString("utf8").includes("locked\n")) {
        locked = true;
        cleanup();
        resolve();
      }
    };
    const onStderr = (chunk: Buffer) => {
      stderr += chunk.toString("utf8");
    };
    const onError = (error: Error) => {
      cleanup();
      reject(error);
    };
    const onExit = (code: number | null, signal: NodeJS.Signals | null) => {
      if (locked) {
        return;
      }
      cleanup();
      reject(new Error(
        `lock helper exited before acquiring lock (code=${code}, signal=${signal}): ${stderr.trim()}`,
      ));
    };

    child.stdout?.on("data", onStdout);
    child.stderr?.on("data", onStderr);
    child.on("error", onError);
    child.on("exit", onExit);
  });
}

async function stopLockProcess(child: ChildProcess): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) {
    return;
  }
  child.stdin?.end();
  await new Promise<void>((resolve, reject) => {
    child.once("exit", () => resolve());
    child.once("error", reject);
  });
}

export async function acquireExclusiveLock(
  name: string,
  options: ExclusiveLockOptions = {},
): Promise<() => Promise<void>> {
  const rootDir = options.rootDir ?? LOCK_ROOT;
  await ensureLockRoot(rootDir);
  const lockPath = exclusiveLockPath(name, rootDir);

  const child = spawnLockProcess(lockPath);
  await waitForLockProcess(child);

  let released = false;
  return async () => {
    if (released) {
      return;
    }
    released = true;
    await stopLockProcess(child);
  };
}
