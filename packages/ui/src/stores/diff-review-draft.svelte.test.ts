import { describe, expect, it, vi } from "vitest";

import type { MiddlemanClient } from "../types.js";
import { createDiffReviewDraftStore } from "./diff-review-draft.svelte.js";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

describe("createDiffReviewDraftStore", () => {
  it("refreshes PR detail after a successful publish", async () => {
    const client = {
      GET: vi.fn(() => Promise.resolve({
        data: {
          comments: [],
          supported_actions: ["comment"],
          native_multiline_ranges: true,
        },
        response: { status: 200, ok: true },
      })),
      POST: vi.fn(() => Promise.resolve({
        response: { status: 200, ok: true },
      })),
    } as unknown as MiddlemanClient;
    const onPublished = vi.fn();
    const store = createDiffReviewDraftStore({ client, onPublished });
    const ref = {
      provider: "forgejo",
      platformHost: "codeberg.org",
      owner: "acme",
      name: "widgets",
      repoPath: "acme/widgets",
    };

    store.setContext(ref, 42, true);
    await Promise.resolve();
    const ok = await store.publish("comment", "summary");

    expect(ok).toBe(true);
    expect(onPublished).toHaveBeenCalledWith(ref, 42);
  });

  it("keeps publish successful when detail refresh fails", async () => {
    const client = {
      GET: vi.fn(() => Promise.resolve({
        data: {
          comments: [],
          supported_actions: ["comment"],
          native_multiline_ranges: true,
        },
        response: { status: 200, ok: true },
      })),
      POST: vi.fn(() => Promise.resolve({
        response: { status: 200, ok: true },
      })),
    } as unknown as MiddlemanClient;
    const store = createDiffReviewDraftStore({
      client,
      onPublished: () => Promise.reject(new Error("refresh failed")),
    });

    store.setContext({
      provider: "forgejo",
      platformHost: "codeberg.org",
      owner: "acme",
      name: "widgets",
      repoPath: "acme/widgets",
    }, 42, true);
    await Promise.resolve();

    await expect(store.publish("comment", "summary")).resolves.toBe(true);
    expect(store.getError()).toBeNull();
  });

  it("does not refresh PR detail when publish fails", async () => {
    const client = {
      GET: vi.fn(() => Promise.resolve({
        data: {
          comments: [],
          supported_actions: ["comment"],
          native_multiline_ranges: true,
        },
        response: { status: 200, ok: true },
      })),
      POST: vi.fn(() => Promise.resolve({
        error: { title: "failed" },
        response: { status: 502, ok: false },
      })),
    } as unknown as MiddlemanClient;
    const onPublished = vi.fn();
    const store = createDiffReviewDraftStore({ client, onPublished });

    store.setContext({
      provider: "forgejo",
      platformHost: "codeberg.org",
      owner: "acme",
      name: "widgets",
      repoPath: "acme/widgets",
    }, 42, true);
    await Promise.resolve();
    await store.publish("comment", "summary");

    expect(onPublished).not.toHaveBeenCalled();
  });

  it("ignores an older same-PR load after publish refreshes the draft", async () => {
    const staleLoad = deferred<unknown>();
    const client = {
      GET: vi
        .fn()
        .mockReturnValueOnce(staleLoad.promise)
        .mockResolvedValueOnce({
          data: {
            comments: [],
            supported_actions: ["comment"],
            native_multiline_ranges: true,
          },
          response: { status: 200, ok: true },
        }),
      POST: vi.fn(() => Promise.resolve({
        response: { status: 200, ok: true },
      })),
    } as unknown as MiddlemanClient;
    const store = createDiffReviewDraftStore({ client });

    store.setContext({
      provider: "forgejo",
      platformHost: "codeberg.org",
      owner: "acme",
      name: "widgets",
      repoPath: "acme/widgets",
    }, 42, true);
    await Promise.resolve();

    await expect(store.publish("comment", "summary")).resolves.toBe(true);
    expect(store.getComments()).toEqual([]);

    staleLoad.resolve({
      data: {
        comments: [{ id: "stale", body: "old draft" }],
        supported_actions: ["comment"],
        native_multiline_ranges: true,
      },
      response: { status: 200, ok: true },
    });
    await staleLoad.promise;
    await Promise.resolve();

    expect(store.getComments()).toEqual([]);
  });
});
