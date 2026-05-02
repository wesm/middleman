import { spawn, type ChildProcess } from "node:child_process";
import { mkdir } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import process from "node:process";

const LOCK_ROOT = path.join(os.tmpdir(), "middleman-playwright-locks");
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

function spawnLockProcess(lockFile: string): ChildProcess {
  return spawn(
    "perl",
    [
      "-MFcntl=:flock",
      "-MIO::Handle",
      "-e",
      `
        my ($path, $metadata) = @ARGV;
        open(my $fh, ">>", $path) or die "open $path: $!";
        flock($fh, LOCK_EX) or die "flock $path: $!";
        truncate($fh, 0) or die "truncate $path: $!";
        seek($fh, 0, 0) or die "seek $path: $!";
        print $fh $metadata or die "write $path: $!";
        $fh->flush();
        print STDOUT "locked\\n";
        STDOUT->flush();
        while (sysread(STDIN, my $buf, 8192)) {}
      `,
      lockFile,
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
  await mkdir(rootDir, { recursive: true });
  const lockFile = exclusiveLockPath(name, rootDir);

  const child = spawnLockProcess(lockFile);
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
