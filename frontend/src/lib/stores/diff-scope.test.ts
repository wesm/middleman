import { describe, it, expect, vi, beforeEach } from "vitest";

const storage = new Map<string, string>();
vi.stubGlobal("localStorage", {
  getItem: (k: string) => storage.get(k) ?? null,
  setItem: (k: string, v: string) => storage.set(k, v),
  removeItem: (k: string) => storage.delete(k),
});

const mockFetch = vi.fn();
vi.stubGlobal("fetch", mockFetch);

import { createDiffStore } from "@middleman/ui/stores/diff";
import type { DiffScope } from "@middleman/ui/stores/diff";

function makeDiffResponse() {
  return {
    stale: false,
    whitespace_only_count: 0,
    files: [{ path: "a.go", old_path: "a.go", status: "modified", is_binary: false, is_whitespace_only: false, additions: 1, deletions: 0, hunks: [] }],
  };
}

function makeCommitsResponse(n: number = 3) {
  const commits = [];
  for (let i = n; i > 0; i--) {
    commits.push({ sha: `sha${i}`, message: `commit ${i}`, author_name: "Alice", authored_at: "2026-01-01T00:00:00Z" });
  }
  return { commits };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("diff store scope", () => {
  let store: ReturnType<typeof createDiffStore>;

  beforeEach(() => {
    storage.clear();
    mockFetch.mockReset();
    store = createDiffStore();
  });

  it("starts at HEAD scope", () => {
    expect(store.getScope()).toEqual({ kind: "head" });
  });

  it("loadCommits fetches and stores commits", async () => {
    mockFetch
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()))
      .mockResolvedValueOnce(jsonResponse(makeCommitsResponse(3)));

    await store.loadDiff("o", "n", 1);
    await store.loadCommits();

    expect(store.getCommits()).toHaveLength(3);
    expect(store.getCommits()![0].sha).toBe("sha3");
  });

  it("loadCommits is a no-op if already loaded", async () => {
    mockFetch
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()))
      .mockResolvedValueOnce(jsonResponse(makeCommitsResponse(3)));

    await store.loadDiff("o", "n", 1);
    await store.loadCommits();
    await store.loadCommits();

    expect(mockFetch).toHaveBeenCalledTimes(2);
  });

  it("selectCommit sets scope and refetches diff", async () => {
    mockFetch
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()))
      .mockResolvedValueOnce(jsonResponse(makeCommitsResponse(3)))
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()));

    await store.loadDiff("o", "n", 1);
    await store.loadCommits();
    store.selectCommit("sha2");

    expect(store.getScope()).toEqual({ kind: "commit", sha: "sha2" });
  });

  it("selectRange orders SHAs by commit index", async () => {
    mockFetch
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()))
      .mockResolvedValueOnce(jsonResponse(makeCommitsResponse(3)))
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()));

    await store.loadDiff("o", "n", 1);
    await store.loadCommits();
    store.selectRange("sha3", "sha1");

    const s = store.getScope() as Extract<DiffScope, { kind: "range" }>;
    expect(s.kind).toBe("range");
    expect(s.fromSha).toBe("sha1");
    expect(s.toSha).toBe("sha3");
  });

  it("resetToHead returns to HEAD and refetches", async () => {
    mockFetch
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()))
      .mockResolvedValueOnce(jsonResponse(makeCommitsResponse(3)))
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()))
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()));

    await store.loadDiff("o", "n", 1);
    await store.loadCommits();
    store.selectCommit("sha2");
    store.resetToHead();

    expect(store.getScope()).toEqual({ kind: "head" });
  });

  it("stepPrev from HEAD goes to newest commit", async () => {
    mockFetch
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()))
      .mockResolvedValueOnce(jsonResponse(makeCommitsResponse(3)))
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()));

    await store.loadDiff("o", "n", 1);
    await store.loadCommits();
    store.stepPrev();

    expect(store.getScope()).toEqual({ kind: "commit", sha: "sha3" });
  });

  it("stepNext from HEAD is a no-op", async () => {
    mockFetch
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()))
      .mockResolvedValueOnce(jsonResponse(makeCommitsResponse(3)));

    await store.loadDiff("o", "n", 1);
    await store.loadCommits();
    store.stepNext();

    expect(store.getScope()).toEqual({ kind: "head" });
  });

  it("stepNext from newest commit returns to HEAD", async () => {
    mockFetch
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()))
      .mockResolvedValueOnce(jsonResponse(makeCommitsResponse(3)))
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()))
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()));

    await store.loadDiff("o", "n", 1);
    await store.loadCommits();
    store.selectCommit("sha3");
    store.stepNext();

    expect(store.getScope()).toEqual({ kind: "head" });
  });

  it("stepPrev from oldest commit is a no-op", async () => {
    mockFetch
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()))
      .mockResolvedValueOnce(jsonResponse(makeCommitsResponse(3)))
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()));

    await store.loadDiff("o", "n", 1);
    await store.loadCommits();
    store.selectCommit("sha1");
    store.stepPrev();

    expect(store.getScope()).toEqual({ kind: "commit", sha: "sha1" });
  });

  it("stepPrev from range collapses to fromSha", async () => {
    mockFetch
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()))
      .mockResolvedValueOnce(jsonResponse(makeCommitsResponse(3)))
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()))
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()));

    await store.loadDiff("o", "n", 1);
    await store.loadCommits();
    store.selectRange("sha1", "sha3");
    store.stepPrev();

    expect(store.getScope()).toEqual({ kind: "commit", sha: "sha1" });
  });

  it("stepNext from range collapses to toSha", async () => {
    mockFetch
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()))
      .mockResolvedValueOnce(jsonResponse(makeCommitsResponse(3)))
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()))
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()));

    await store.loadDiff("o", "n", 1);
    await store.loadCommits();
    store.selectRange("sha1", "sha3");
    store.stepNext();

    expect(store.getScope()).toEqual({ kind: "commit", sha: "sha3" });
  });

  it("diff fetch includes commit param when scope is single commit", async () => {
    mockFetch
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()))
      .mockResolvedValueOnce(jsonResponse(makeCommitsResponse(3)))
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()));

    await store.loadDiff("o", "n", 1);
    await store.loadCommits();
    store.selectCommit("sha2");

    await vi.waitFor(() => {
      const lastCall = mockFetch.mock.calls[mockFetch.mock.calls.length - 1];
      const url = lastCall[0] as string;
      expect(url).toContain("commit=sha2");
    });
  });

  it("diff fetch includes from+to params when scope is range", async () => {
    mockFetch
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()))
      .mockResolvedValueOnce(jsonResponse(makeCommitsResponse(3)))
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()));

    await store.loadDiff("o", "n", 1);
    await store.loadCommits();
    store.selectRange("sha1", "sha3");

    await vi.waitFor(() => {
      const lastCall = mockFetch.mock.calls[mockFetch.mock.calls.length - 1];
      const url = lastCall[0] as string;
      expect(url).toContain("from=sha1");
      expect(url).toContain("to=sha3");
    });
  });

  it("clearDiff resets scope and commits", async () => {
    mockFetch
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()))
      .mockResolvedValueOnce(jsonResponse(makeCommitsResponse(3)))
      .mockResolvedValueOnce(jsonResponse(makeDiffResponse()));

    await store.loadDiff("o", "n", 1);
    await store.loadCommits();
    store.selectCommit("sha2");
    store.clearDiff();

    expect(store.getScope()).toEqual({ kind: "head" });
    expect(store.getCommits()).toBeNull();
  });
});
