import { afterEach, describe, expect, it, vi } from "vitest";
import { createDiffStore } from "@middleman/ui/stores/diff";
import type { DiffStoreOptions } from "@middleman/ui/stores/diff";
import type { DiffResult, FilesResult } from "@middleman/ui/api/types";

type TestClient = NonNullable<DiffStoreOptions["client"]>;

interface TestGetOptions {
  params?: {
    path?: Record<string, string | number>;
    query?: Record<string, string | number | boolean | undefined>;
  };
  signal?: AbortSignal;
}

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

function testClient(): TestClient {
  return {
    GET: vi.fn(
      async (path: string, options?: TestGetOptions) => {
        const response = await globalThis.fetch(
          testURL(path, options),
          options?.signal ? { signal: options.signal } : undefined,
        );
        if (!response.ok) {
          return {
            error: await response.json().catch(() => ({})),
            response,
          };
        }
        return {
          data: await response.json(),
          response,
        };
      },
    ),
  } as unknown as TestClient;
}

function testURL(
  path: string,
  options?: TestGetOptions,
): string {
  let url = `/api/v1${path}`;
  for (const [key, value] of Object.entries(options?.params?.path ?? {})) {
    url = url.replace(
      `{${key}}`,
      encodeURIComponent(String(value)),
    );
  }
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(options?.params?.query ?? {})) {
    if (value !== undefined) query.set(key, String(value));
  }
  const qs = query.toString();
  return qs ? `${url}?${qs}` : url;
}

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.removeItem("diff-hide-whitespace");
  localStorage.removeItem("diff-tab-width");
  localStorage.removeItem("diff-collapsed-files");
});

describe("createDiffStore loadDiff", () => {
  it("loads workspace diffs with the selected base", async () => {
    const calls: string[] = [];
    const files = makeFilesResult([
      "src/app.go",
      "src/app_test.go",
      "docs/plan.md",
    ]);
    const diff = makeDiffResult([
      "src/app.go",
      "src/app_test.go",
      "docs/plan.md",
    ]);

    vi.spyOn(globalThis, "fetch").mockImplementation(
      async (input: RequestInfo | URL) => {
        const url =
          typeof input === "string"
            ? input
            : input instanceof URL
              ? input.href
              : input.url;
        calls.push(url);
        if (url.includes("/workspaces/ws-1/files")) {
          return Response.json(files);
        }
        if (url.includes("/workspaces/ws-1/diff")) {
          return Response.json(diff);
        }
        return Response.json({}, { status: 404 });
      },
    );

    const store = createDiffStore({ client: testClient() });

    await store.loadWorkspaceDiff("ws-1", "origin");

    expect(calls).toContain("/api/v1/workspaces/ws-1/files?base=origin");
    expect(calls).toContain("/api/v1/workspaces/ws-1/diff?base=origin");
    expect(store.getFileCategoryCounts()).toEqual({
      all: 3,
      plansDocs: 1,
      code: 1,
      tests: 1,
      other: 0,
    });
  });

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

    const store = createDiffStore({ client: testClient() });

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
        const signal = input instanceof Request ? input.signal : init?.signal;

        if (url.includes("pulls/1/files")) {
          return new Promise((_resolve, reject) => {
            signal?.addEventListener("abort", () => {
              filesAAborted = true;
              reject(new DOMException("Aborted", "AbortError"));
            });
          });
        }
        if (url.includes("pulls/1/diff")) {
          return new Promise((_resolve, reject) => {
            signal?.addEventListener("abort", () => {
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

    const store = createDiffStore({ client: testClient() });

    // Start loading PR A (will hang).
    void store.loadDiff("owner", "repo", 1);

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

    const store = createDiffStore({ client: testClient() });
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
    const store = createDiffStore({ client: testClient() });
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

    const store = createDiffStore({ client: testClient() });
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

    const store = createDiffStore({ client: testClient() });
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

    const store = createDiffStore({ client: testClient() });
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

    const store = createDiffStore({ client: testClient() });
    await store.loadDiff("owner", "repo", 1);

    // getFileList must return [] not null, even when API sends null.
    const result = store.getFileList();
    expect(result).not.toBeNull();
    expect(result!.files).toEqual([]);
  });

  it("filters loaded diff and file list by selected file category", async () => {
    const result: DiffResult = {
      stale: false,
      whitespace_only_count: 0,
      files: [
        {
          path: "docs/review-plan.md",
          old_path: "docs/review-plan.md",
          status: "modified",
          is_binary: false,
          is_whitespace_only: false,
          additions: 1,
          deletions: 1,
          hunks: [],
        },
        {
          path: "src/App.svelte",
          old_path: "src/App.svelte",
          status: "modified",
          is_binary: false,
          is_whitespace_only: false,
          additions: 1,
          deletions: 1,
          hunks: [],
        },
        {
          path: "src/App.test.ts",
          old_path: "src/App.test.ts",
          status: "modified",
          is_binary: false,
          is_whitespace_only: false,
          additions: 1,
          deletions: 1,
          hunks: [],
        },
        {
          path: "bun.lock",
          old_path: "bun.lock",
          status: "modified",
          is_binary: false,
          is_whitespace_only: false,
          additions: 1,
          deletions: 1,
          hunks: [],
        },
      ],
    };

    vi.spyOn(globalThis, "fetch").mockImplementation(
      async (input: RequestInfo | URL) => {
        const url =
          typeof input === "string"
            ? input
            : input instanceof URL
              ? input.href
              : input.url;

        if (url.includes("/files")) {
          return Response.json({ stale: false, files: result.files });
        }
        if (url.includes("/diff")) {
          return Response.json(result);
        }
        return Response.json({}, { status: 404 });
      },
    );

    const store = createDiffStore({ client: testClient() });
    await store.loadDiff("owner", "repo", 1);

    expect(store.getFileCategoryFilter()).toBe("all");
    expect(store.getVisibleDiffFiles().map((file) => file.path)).toEqual([
      "docs/review-plan.md",
      "src/App.svelte",
      "src/App.test.ts",
      "bun.lock",
    ]);
    expect(store.getFileCategoryCounts()).toEqual({
      plansDocs: 1,
      code: 1,
      tests: 1,
      other: 1,
      all: 4,
    });

    store.setFileCategoryFilter("tests");

    expect(store.getVisibleDiffFiles().map((file) => file.path)).toEqual([
      "src/App.test.ts",
    ]);
    expect(store.getVisibleFileList()?.files.map((file) => file.path)).toEqual([
      "src/App.test.ts",
    ]);
  });
});
