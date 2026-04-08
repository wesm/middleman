import { describe, expect, it, vi } from "vitest";

import { createIssuesStore } from "@middleman/ui/stores/issues";
import type { MiddlemanClient } from "@middleman/ui";

interface MockIssueDetail {
  repo_owner: string;
  repo_name: string;
  issue: { Number: number };
  events: unknown[];
}

function makeDetail(
  events: unknown[] = [],
  number = 1,
): MockIssueDetail {
  return {
    repo_owner: "octo",
    repo_name: "repo",
    issue: { Number: number },
    events,
  };
}

describe("createIssuesStore submitIssueComment", () => {
  it("refreshes the issues list after posting a comment when on the issues page", async () => {
    const detailData = makeDetail();
    const getCalls: string[] = [];
    const client = {
      GET: vi.fn(async (path: string) => {
        getCalls.push(path);
        if (path === "/issues") return { data: [] };
        return { data: detailData };
      }),
      POST: vi.fn(async (path: string) => {
        if (path.includes("/sync")) return { data: detailData };
        if (path.includes("/comments")) return { data: { ID: 42 } };
        return { data: undefined };
      }),
      PUT: vi.fn(),
      DELETE: vi.fn(),
    } as unknown as MiddlemanClient;

    const store = createIssuesStore({
      client,
      getPage: () => "issues",
    });

    await store.loadIssueDetail("octo", "repo", 1);
    // Drain the background syncIssueDetail fired by loadIssueDetail.
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
    const listCallsBefore = getCalls.filter(
      (p) => p === "/issues",
    ).length;

    await store.submitIssueComment("octo", "repo", 1, "hi");
    // Drain the background syncIssueDetail fired by submitIssueComment.
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();

    const listCallsAfter = getCalls.filter(
      (p) => p === "/issues",
    ).length;
    expect(listCallsAfter).toBeGreaterThan(listCallsBefore);
  });

  it("does not refresh the issues list when on a different page", async () => {
    const detailData = makeDetail();
    const getCalls: string[] = [];
    const client = {
      GET: vi.fn(async (path: string) => {
        getCalls.push(path);
        return { data: detailData };
      }),
      POST: vi.fn(async (path: string) => {
        if (path.includes("/sync")) return { data: detailData };
        if (path.includes("/comments")) return { data: { ID: 42 } };
        return { data: undefined };
      }),
      PUT: vi.fn(),
      DELETE: vi.fn(),
    } as unknown as MiddlemanClient;

    const store = createIssuesStore({
      client,
      getPage: () => "pulls",
    });

    await store.loadIssueDetail("octo", "repo", 1);
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
    await store.submitIssueComment("octo", "repo", 1, "hi");
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();

    expect(getCalls.some((p) => p === "/issues")).toBe(false);
  });
});
