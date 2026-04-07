import { describe, expect, it, vi } from "vitest";

import { createDetailStore } from "@middleman/ui/stores/detail";
import type { MiddlemanClient } from "@middleman/ui";

interface MockDetail {
  repo_owner: string;
  repo_name: string;
  merge_request: { Number: number };
  events: unknown[];
}

function makeDetail(events: unknown[] = []): MockDetail {
  return {
    repo_owner: "octo",
    repo_name: "repo",
    merge_request: { Number: 1 },
    events,
  };
}

describe("createDetailStore submitComment", () => {
  it("never flips loading flag while refreshing after a comment", async () => {
    const detailData = makeDetail();
    const loadingDuringRefresh: boolean[] = [];
    let getCallCount = 0;
    const holder: {
      store: ReturnType<typeof createDetailStore> | null;
    } = { store: null };

    const client = {
      GET: vi.fn(async () => {
        getCallCount++;
        if (getCallCount > 1 && holder.store) {
          loadingDuringRefresh.push(holder.store.isDetailLoading());
        }
        return { data: detailData };
      }),
      POST: vi.fn(async (path: string) => {
        if (path.includes("/sync")) {
          return { data: detailData };
        }
        if (path.includes("/comments")) {
          return { data: { ID: 42 } };
        }
        return { data: undefined };
      }),
      PUT: vi.fn(),
      DELETE: vi.fn(),
    } as unknown as MiddlemanClient;

    holder.store = createDetailStore({ client });

    await holder.store.loadDetail("octo", "repo", 1);
    // Allow background syncDetail microtasks to settle.
    await Promise.resolve();
    await Promise.resolve();

    await holder.store.submitComment("octo", "repo", 1, "hello");

    expect(getCallCount).toBeGreaterThan(1);
    expect(loadingDuringRefresh.length).toBeGreaterThan(0);
    expect(loadingDuringRefresh.every((v) => v === false)).toBe(true);
    expect(holder.store.isDetailLoading()).toBe(false);
  });

  it("discards stale syncDetail responses after posting a comment", async () => {
    const staleDetail = makeDetail([]);
    const freshDetail = makeDetail([{ ID: 42, Kind: "comment" }]);

    let syncResolve: (value: unknown) => void = () => {};
    const syncPromise = new Promise((resolve) => {
      syncResolve = resolve;
    });

    let getCallCount = 0;
    const client = {
      GET: vi.fn(async () => {
        getCallCount++;
        // First call: initial loadDetail — still no comment.
        // Second call: refreshDetail inside submitComment — comment present.
        if (getCallCount === 1) return { data: staleDetail };
        return { data: freshDetail };
      }),
      POST: vi.fn(async (path: string) => {
        if (path.includes("/sync")) return await syncPromise;
        if (path.includes("/comments")) return { data: { ID: 42 } };
        return { data: undefined };
      }),
      PUT: vi.fn(),
      DELETE: vi.fn(),
    } as unknown as MiddlemanClient;

    const store = createDetailStore({ client });

    // loadDetail resolves after the initial GET, but fires a background
    // syncDetail that is still blocked on syncPromise.
    await store.loadDetail("octo", "repo", 1);

    // submitComment refreshes silently and should pick up the new event.
    await store.submitComment("octo", "repo", 1, "hello");
    expect(store.getDetail()?.events).toHaveLength(1);

    // The background sync now returns stale data (no comment).
    // It must be discarded rather than overwrite the fresh detail.
    syncResolve({ data: staleDetail, error: undefined });
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();

    expect(store.getDetail()?.events).toHaveLength(1);
  });
});
