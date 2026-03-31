import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Mock router — getPage() returns "pulls" so refreshPullsIfActive fires.
vi.mock("../stores/router.svelte.js", () => ({
  getPage: () => "pulls",
}));

// Deferred helpers to control when API calls resolve.
type Deferred<T> = {
  promise: Promise<T>;
  resolve: (v: T) => void;
  reject: (e: unknown) => void;
};
function deferred<T>(): Deferred<T> {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type APIResult = { data?: any; error?: { detail: string } };

// Track calls to the mock client.
const putCalls: Deferred<APIResult>[] = [];
const getCalls: Deferred<APIResult>[] = [];

vi.mock("../api/runtime.js", () => ({
  client: {
    PUT: vi.fn(() => {
      const d = deferred<APIResult>();
      putCalls.push(d);
      return d.promise;
    }),
    GET: vi.fn(() => {
      const d = deferred<APIResult>();
      getCalls.push(d);
      return d.promise;
    }),
  },
}));

const {
  getDetail,
  loadDetail,
  updateKanbanState,
} = await import("./detail.svelte.js");
const { getPulls, loadPulls } = await import("./pulls.svelte.js");

/** Flush microtasks so async continuations run. */
function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

function makePR(
  owner: string,
  name: string,
  number: number,
  status: string,
) {
  return {
    ID: number,
    Number: number,
    Title: `PR #${number}`,
    Author: "user",
    AuthorDisplayName: "User",
    State: "open",
    IsDraft: false,
    Body: "",
    URL: "",
    Additions: 0,
    Deletions: 0,
    CreatedAt: "2025-01-01T00:00:00Z",
    UpdatedAt: "2025-01-01T00:00:00Z",
    LastActivityAt: "2025-01-01T00:00:00Z",
    KanbanStatus: status,
    Starred: false,
    CIStatus: "",
    CIChecksJSON: "",
    ReviewDecision: "",
    repo_owner: owner,
    repo_name: name,
  };
}

function makeDetailResponse(
  owner: string,
  name: string,
  number: number,
  status: string,
): APIResult {
  return {
    data: {
      repo_owner: owner,
      repo_name: name,
      pull_request: makePR(owner, name, number, status),
      events: [],
    },
  };
}

/** Seed both stores with initial state. */
async function seedStores(
  prs: { owner: string; name: string; number: number; status: string }[],
): Promise<void> {
  // Seed detail store with the first PR.
  const first = prs[0]!;
  const detailP = loadDetail(first.owner, first.name, first.number);
  getCalls[getCalls.length - 1]!.resolve(
    makeDetailResponse(first.owner, first.name, first.number, first.status),
  );
  await detailP;

  // Seed pulls store with all PRs.
  const pullsP = loadPulls();
  getCalls[getCalls.length - 1]!.resolve({
    data: prs.map((p) => makePR(p.owner, p.name, p.number, p.status)),
  });
  await pullsP;
}

describe("updateKanbanState", () => {
  beforeEach(() => {
    putCalls.length = 0;
    getCalls.length = 0;
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("stale failure does not overwrite newer optimistic state", async () => {
    await seedStores([{ owner: "acme", name: "repo", number: 42, status: "new" }]);
    expect(getDetail()!.pull_request.KanbanStatus).toBe("new");
    expect(getPulls()[0]!.KanbanStatus).toBe("new");

    // Fire request A (will fail) and request B (will succeed).
    const promiseA = updateKanbanState("acme", "repo", 42, "reviewing");
    const promiseB = updateKanbanState("acme", "repo", 42, "waiting");

    // Both optimistic updates applied; latest wins.
    expect(getDetail()!.pull_request.KanbanStatus).toBe("waiting");
    expect(getPulls()[0]!.KanbanStatus).toBe("waiting");

    // Fail request A. Because A is stale (B incremented the
    // counter for this PR), the failure handler is a no-op.
    putCalls[0]!.resolve({ error: { detail: "server error" } });
    await promiseA;

    expect(getDetail()!.pull_request.KanbanStatus).toBe("waiting");
    expect(getPulls()[0]!.KanbanStatus).toBe("waiting");

    // Succeed request B. Don't await yet — B's success path calls
    // loadPulls() which issues a GET we must resolve first.
    putCalls[1]!.resolve({});
    await flush();

    // Resolve the GET /pulls that B's success path triggered.
    getCalls[getCalls.length - 1]!.resolve({
      data: [makePR("acme", "repo", 42, "waiting")],
    });
    await promiseB;

    expect(getDetail()!.pull_request.KanbanStatus).toBe("waiting");
    expect(getPulls()[0]!.KanbanStatus).toBe("waiting");
  });

  it("failed update on PR A still reconciles when PR B is updated concurrently", async () => {
    // Detail pane shows PR 42; both PRs are in the pulls list.
    await seedStores([
      { owner: "acme", name: "repo", number: 42, status: "new" },
      { owner: "acme", name: "repo", number: 99, status: "new" },
    ]);

    // Update PR 42 (will fail) and PR 99 (will succeed).
    const promiseA = updateKanbanState("acme", "repo", 42, "reviewing");
    const promiseB = updateKanbanState("acme", "repo", 99, "waiting");

    // Optimistic updates applied independently.
    expect(getPulls().find((p) => p.Number === 42)!.KanbanStatus).toBe("reviewing");
    expect(getPulls().find((p) => p.Number === 99)!.KanbanStatus).toBe("waiting");

    // Detail pane shows PR 42, so its optimistic update applied there.
    expect(getDetail()!.pull_request.KanbanStatus).toBe("reviewing");
    // PR 99's update must NOT have mutated the detail pane.
    expect(getDetail()!.pull_request.Number).toBe(42);

    // Fail PR 42's PUT. Its counter is still current (PR 99's
    // update uses a different key), so recovery should fire.
    putCalls[0]!.resolve({ error: { detail: "server error" } });
    await flush();

    // The failure path reloads pulls then detail (PR 42 is shown).
    // Resolve both GETs in the order they were issued.
    const getCountBeforeResolve = getCalls.length;
    // First GET: loadPulls
    getCalls[getCountBeforeResolve - 2]!.resolve({
      data: [
        makePR("acme", "repo", 42, "new"),
        makePR("acme", "repo", 99, "new"),
      ],
    });
    // Second GET: loadDetail for PR 42
    getCalls[getCountBeforeResolve - 1]!.resolve(
      makeDetailResponse("acme", "repo", 42, "new"),
    );
    await promiseA;

    // PR 42 rolled back to server state in both stores.
    expect(getDetail()!.pull_request.KanbanStatus).toBe("new");
    expect(getPulls().find((p) => p.Number === 42)!.KanbanStatus).toBe("new");

    // Succeed PR 99's PUT.
    putCalls[1]!.resolve({});
    await flush();

    // PR 99's success path refreshes pulls only (detail shows PR 42).
    getCalls[getCalls.length - 1]!.resolve({
      data: [
        makePR("acme", "repo", 42, "new"),
        makePR("acme", "repo", 99, "waiting"),
      ],
    });
    await promiseB;

    // Detail pane still shows PR 42, unchanged by PR 99's update.
    expect(getDetail()!.pull_request.Number).toBe(42);
    expect(getDetail()!.pull_request.KanbanStatus).toBe("new");
    expect(getPulls().find((p) => p.Number === 42)!.KanbanStatus).toBe("new");
    expect(getPulls().find((p) => p.Number === 99)!.KanbanStatus).toBe("waiting");
  });

  it("older success reconciles after newer failure on same PR", async () => {
    await seedStores([{ owner: "acme", name: "repo", number: 42, status: "new" }]);

    // Fire A then B for the same PR.
    const promiseA = updateKanbanState("acme", "repo", 42, "reviewing");
    const promiseB = updateKanbanState("acme", "repo", 42, "waiting");

    // B's optimistic update is latest.
    expect(getDetail()!.pull_request.KanbanStatus).toBe("waiting");
    expect(getPulls()[0]!.KanbanStatus).toBe("waiting");

    // B fails first. Its recovery runs (local rollback + reload).
    putCalls[1]!.resolve({ error: { detail: "server error" } });
    await flush();

    // Resolve the reloads that B's failure triggers.
    // loadPulls GET:
    getCalls[getCalls.length - 2]!.resolve({
      data: [makePR("acme", "repo", 42, "new")],
    });
    // loadDetail GET:
    getCalls[getCalls.length - 1]!.resolve(
      makeDetailResponse("acme", "repo", 42, "new"),
    );
    await promiseB;

    // After B's recovery, state is back to server truth.
    expect(getDetail()!.pull_request.KanbanStatus).toBe("new");
    expect(getPulls()[0]!.KanbanStatus).toBe("new");

    // A succeeds later. Its handler must NOT be suppressed; the
    // server accepted "reviewing" so we need to refresh.
    putCalls[0]!.resolve({});
    await flush();

    // A's success triggers refreshPullsIfActive → loadPulls.
    getCalls[getCalls.length - 1]!.resolve({
      data: [makePR("acme", "repo", 42, "reviewing")],
    });
    await promiseA;

    expect(getDetail()!.pull_request.KanbanStatus).toBe("new");
    expect(getPulls()[0]!.KanbanStatus).toBe("reviewing");
  });

  it("two same-PR successes resolving out of order converge to server state", async () => {
    await seedStores([{ owner: "acme", name: "repo", number: 42, status: "new" }]);

    // Fire A then B for the same PR; both will succeed.
    const promiseA = updateKanbanState("acme", "repo", 42, "reviewing");
    const promiseB = updateKanbanState("acme", "repo", 42, "waiting");

    expect(getPulls()[0]!.KanbanStatus).toBe("waiting");

    // B succeeds first.
    putCalls[1]!.resolve({});
    await flush();

    // B's success refreshes pulls from server (server has "waiting").
    getCalls[getCalls.length - 1]!.resolve({
      data: [makePR("acme", "repo", 42, "waiting")],
    });
    await promiseB;

    expect(getPulls()[0]!.KanbanStatus).toBe("waiting");

    // A succeeds later. Server applied A before B, so server still
    // has "waiting". A's refresh must still run and converge.
    putCalls[0]!.resolve({});
    await flush();

    getCalls[getCalls.length - 1]!.resolve({
      data: [makePR("acme", "repo", 42, "waiting")],
    });
    await promiseA;

    expect(getPulls()[0]!.KanbanStatus).toBe("waiting");
  });
});
