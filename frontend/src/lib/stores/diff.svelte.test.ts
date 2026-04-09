import { afterEach, describe, expect, it, vi } from "vitest";
import { createDiffStore } from "@middleman/ui/stores/diff";
import type { DiffResult, FilesResult } from "@middleman/ui/api/types";

function makeDiffResult(files: string[]): DiffResult {
  return {
    stale: false,
    whitespace_only_count: 0,
    files: files.map((path) => ({
      path,
      old_path: path,
      status: "modified" as const,
      is_binary: false,
      is_whitespace_only: false,
      additions: 1,
      deletions: 1,
      hunks: [],
    })),
  };
}

function makeFilesResult(files: string[]): FilesResult {
  return {
    stale: false,
    files: files.map((path) => ({
      path,
      old_path: path,
      status: "modified" as const,
      is_binary: false,
      is_whitespace_only: false,
      additions: 0,
      deletions: 0,
      hunks: [],
    })),
  };
}

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.removeItem("diff-hide-whitespace");
  localStorage.removeItem("diff-tab-width");
  localStorage.removeItem("diff-collapsed-files");
});

describe("createDiffStore loadDiff", () => {
  it("clears stale data when switching PRs", async () => {
    const filesA = makeFilesResult(["a.ts"]);
    const diffA = makeDiffResult(["a.ts"]);
    const filesB = makeFilesResult(["b.ts"]);
    const diffB = makeDiffResult(["b.ts"]);

    // Deferred responses to control resolution order.
    let resolveFilesB: () => void = () => {};
    let resolveDiffB: () => void = () => {};

    vi.spyOn(globalThis, "fetch").mockImplementation(
      async (input: RequestInfo | URL) => {
        const url =
          typeof input === "string"
            ? input
            : input instanceof URL
              ? input.href
              : input.url;

        // PR A fetches resolve immediately.
        if (url.includes("pulls/1/files")) {
          return Response.json(filesA);
        }
        if (url.includes("pulls/1/diff")) {
          return Response.json(diffA);
        }
        // PR B: both deferred so we control timing explicitly.
        if (url.includes("pulls/2/files")) {
          return new Promise((resolve) => {
            resolveFilesB = () => resolve(Response.json(filesB));
          });
        }
        if (url.includes("pulls/2/diff")) {
          return new Promise((resolve) => {
            resolveDiffB = () => resolve(Response.json(diffB));
          });
        }
        return Response.json({}, { status: 404 });
      },
    );

    const store = createDiffStore({ getBasePath: () => "/" });

    // Load PR A fully.
    await store.loadDiff("owner", "repo", 1);
    expect(store.getDiff()?.files[0]?.path).toBe("a.ts");
    expect(store.getFileList()?.files[0]?.path).toBe("a.ts");

    // Start loading PR B — don't await yet.
    const loadB = store.loadDiff("owner", "repo", 2);

    // Both stale PR A values must be null immediately.
    expect(store.getDiff()).toBeNull();
    expect(store.getFileList()).toBeNull();

    // Release /files for B and let it settle.
    resolveFilesB();
    await vi.waitFor(() => {
      expect(store.getFileList()?.files[0]?.path).toBe("b.ts");
    });

    // Diff still null (not yet resolved).
    expect(store.getDiff()).toBeNull();

    // Release /diff for B.
    resolveDiffB();
    await loadB;

    expect(store.getDiff()?.files[0]?.path).toBe("b.ts");
    expect(store.getFileList()?.files[0]?.path).toBe("b.ts");
  });

  it("aborts in-flight requests when switching PRs", async () => {
    const filesA = makeFilesResult(["a.ts"]);
    const diffB = makeDiffResult(["b.ts"]);
    const filesB = makeFilesResult(["b.ts"]);

    let diffAAborted = false;
    let filesAAborted = false;

    vi.spyOn(globalThis, "fetch").mockImplementation(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url =
          typeof input === "string"
            ? input
            : input instanceof URL
              ? input.href
              : input.url;

        if (url.includes("pulls/1/files")) {
          return new Promise((_resolve, reject) => {
            init?.signal?.addEventListener("abort", () => {
              filesAAborted = true;
              reject(new DOMException("Aborted", "AbortError"));
            });
          });
        }
        if (url.includes("pulls/1/diff")) {
          return new Promise((_resolve, reject) => {
            init?.signal?.addEventListener("abort", () => {
              diffAAborted = true;
              reject(new DOMException("Aborted", "AbortError"));
            });
          });
        }
        if (url.includes("pulls/2/files")) {
          return Response.json(filesB);
        }
        if (url.includes("pulls/2/diff")) {
          return Response.json(diffB);
        }
        return Response.json({}, { status: 404 });
      },
    );

    const store = createDiffStore({ getBasePath: () => "/" });

    // Start loading PR A (will hang).
    const loadA = store.loadDiff("owner", "repo", 1);

    // Switch to PR B — should abort PR A.
    await store.loadDiff("owner", "repo", 2);

    expect(diffAAborted).toBe(true);
    expect(filesAAborted).toBe(true);
    expect(store.getDiff()?.files[0]?.path).toBe("b.ts");
  });

  it("shows loading when /files fails but /diff still in flight", async () => {
    const diff = makeDiffResult(["a.ts"]);
    let resolveDiff: () => void = () => {};

    vi.spyOn(globalThis, "fetch").mockImplementation(
      async (input: RequestInfo | URL) => {
        const url =
          typeof input === "string"
            ? input
            : input instanceof URL
              ? input.href
              : input.url;

        if (url.includes("/files")) {
          return Response.json({ detail: "server error" }, { status: 500 });
        }
        if (url.includes("/diff")) {
          return new Promise((resolve) => {
            resolveDiff = () => resolve(Response.json(diff));
          });
        }
        return Response.json({}, { status: 404 });
      },
    );

    const store = createDiffStore({ getBasePath: () => "/" });
    const loadP = store.loadDiff("owner", "repo", 1);

    // Wait for /files to fail.
    await vi.waitFor(() => {
      expect(store.getFileList()).toBeNull();
    });

    // isFileListLoading must stay true — /diff is still in flight.
    expect(store.isFileListLoading()).toBe(true);

    // Resolve /diff — file list falls through to diff.files.
    resolveDiff();
    await loadP;

    expect(store.isFileListLoading()).toBe(false);
    expect(store.getFileList()?.files[0]?.path).toBe("a.ts");
  });

  it("prefers diff.files over /files for whitespace filtering", async () => {
    // /files returns all files including whitespace-only ones.
    const filesResult = makeFilesResult(["a.ts", "b.ts"]);
    // /diff with whitespace=hide filters out whitespace-only file.
    const diffResult = makeDiffResult(["a.ts"]);

    const fetchedUrls: string[] = [];
    let resolveFiles: () => void = () => {};
    let resolveDiff: () => void = () => {};

    vi.spyOn(globalThis, "fetch").mockImplementation(
      async (input: RequestInfo | URL) => {
        const url =
          typeof input === "string"
            ? input
            : input instanceof URL
              ? input.href
              : input.url;
        fetchedUrls.push(url);

        if (url.includes("/files")) {
          return new Promise((resolve) => {
            resolveFiles = () => resolve(Response.json(filesResult));
          });
        }
        if (url.includes("/diff")) {
          return new Promise((resolve) => {
            resolveDiff = () => resolve(Response.json(diffResult));
          });
        }
        return Response.json({}, { status: 404 });
      },
    );

    // Enable whitespace hiding before loading.
    localStorage.setItem("diff-hide-whitespace", "true");
    const store = createDiffStore({ getBasePath: () => "/" });
    const loadP = store.loadDiff("owner", "repo", 1);

    // Verify /diff request includes whitespace=hide query param.
    expect(fetchedUrls.some((u) => u.includes("diff?whitespace=hide"))).toBe(
      true,
    );

    // /files arrives first — shows unfiltered preview.
    resolveFiles();
    await vi.waitFor(() => {
      expect(store.getFileList()?.files).toHaveLength(2);
    });

    // /diff arrives — authoritative, whitespace-filtered.
    resolveDiff();
    await loadP;

    expect(store.getFileList()?.files).toHaveLength(1);
    expect(store.getFileList()?.files[0]?.path).toBe("a.ts");
  });

  it("does not fall back to stale /files preview after whitespace toggle fails", async () => {
    const filesResult = makeFilesResult(["a.ts", "b.ts"]);
    const diffAll = makeDiffResult(["a.ts", "b.ts"]);

    vi.spyOn(globalThis, "fetch").mockImplementation(
      async (input: RequestInfo | URL) => {
        const url =
          typeof input === "string"
            ? input
            : input instanceof URL
              ? input.href
              : input.url;

        if (url.includes("/files")) {
          return Response.json(filesResult);
        }
        if (url.includes("/diff")) {
          if (url.includes("whitespace=hide")) {
            // Whitespace-filtered diff request fails.
            return Response.json(
              { detail: "server error" },
              { status: 500 },
            );
          }
          return Response.json(diffAll);
        }
        return Response.json({}, { status: 404 });
      },
    );

    const store = createDiffStore({ getBasePath: () => "/" });
    await store.loadDiff("owner", "repo", 1);
    expect(store.getFileList()?.files).toHaveLength(2);

    // Toggle whitespace — /diff reload will fail.
    store.setHideWhitespace(true);
    await vi.waitFor(() => {
      expect(store.getDiffError()).toBeTruthy();
    });

    // fileList was cleared by reloadDiffOnly, diff is null from error.
    // Sidebar must NOT fall back to stale unfiltered /files preview.
    expect(store.getFileList()).toBeNull();
  });

  it("clears file list when /diff fails so sidebar shows no stale files", async () => {
    const filesResult = makeFilesResult(["a.ts", "b.ts"]);

    vi.spyOn(globalThis, "fetch").mockImplementation(
      async (input: RequestInfo | URL) => {
        const url =
          typeof input === "string"
            ? input
            : input instanceof URL
              ? input.href
              : input.url;

        if (url.includes("/files")) {
          return Response.json(filesResult);
        }
        if (url.includes("/diff")) {
          return Response.json({ detail: "server error" }, { status: 500 });
        }
        return Response.json({}, { status: 404 });
      },
    );

    const store = createDiffStore({ getBasePath: () => "/" });
    await store.loadDiff("owner", "repo", 1);

    // /diff failed — sidebar must not show stale /files data.
    expect(store.getDiffError()).toBeTruthy();
    expect(store.getFileList()).toBeNull();
  });

  it("clears file list when /diff fails before /files resolves", async () => {
    const filesResult = makeFilesResult(["a.ts"]);
    let resolveFiles: () => void = () => {};

    vi.spyOn(globalThis, "fetch").mockImplementation(
      async (input: RequestInfo | URL) => {
        const url =
          typeof input === "string"
            ? input
            : input instanceof URL
              ? input.href
              : input.url;

        if (url.includes("/files")) {
          return new Promise((resolve) => {
            resolveFiles = () => resolve(Response.json(filesResult));
          });
        }
        if (url.includes("/diff")) {
          // /diff fails immediately.
          return Response.json({ detail: "server error" }, { status: 500 });
        }
        return Response.json({}, { status: 404 });
      },
    );

    const store = createDiffStore({ getBasePath: () => "/" });
    const loadP = store.loadDiff("owner", "repo", 1);

    // /diff fails fast, /files still pending — release it.
    resolveFiles();
    await loadP;

    // Late /files must not repopulate sidebar after /diff error.
    expect(store.getDiffError()).toBeTruthy();
    expect(store.getFileList()).toBeNull();
  });

  it("normalizes null files from API to empty array", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      async (input: RequestInfo | URL) => {
        const url =
          typeof input === "string"
            ? input
            : input instanceof URL
              ? input.href
              : input.url;

        if (url.includes("/files")) {
          // API returns files: null (Go nil slice serialization).
          return Response.json({ stale: false, files: null });
        }
        if (url.includes("/diff")) {
          return Response.json({
            stale: false,
            whitespace_only_count: 0,
            files: null,
          });
        }
        return Response.json({}, { status: 404 });
      },
    );

    const store = createDiffStore({ getBasePath: () => "/" });
    await store.loadDiff("owner", "repo", 1);

    // getFileList must return [] not null, even when API sends null.
    const result = store.getFileList();
    expect(result).not.toBeNull();
    expect(result!.files).toEqual([]);
  });
});
