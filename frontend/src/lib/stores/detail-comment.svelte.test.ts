import { describe, expect, it, vi } from "vitest";

import { createDetailStore } from "@middleman/ui/stores/detail";
import type { MiddlemanClient } from "@middleman/ui";

interface MockDetail {
  repo_owner: string;
  repo_name: string;
  merge_request: { Number: number };
  events: unknown[];
}

function makeDetail(): MockDetail {
  return {
    repo_owner: "octo",
    repo_name: "repo",
    merge_request: { Number: 1 },
    events: [],
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
});
