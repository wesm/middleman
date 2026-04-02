import { afterEach, describe, expect, it, vi } from "vitest";
import {
  getPullRequestActions,
  getIssueActions,
  invokeAction,
} from "./embedding-hooks.svelte.js";
import type { ActionHook } from "./embedding-hooks.svelte.js";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const win = window as any;

afterEach(() => {
  win.__middleman_hooks = null;
});

describe("getPullRequestActions", () => {
  it("returns empty array when no hooks registered", () => {
    expect(getPullRequestActions()).toEqual([]);
  });

  it("returns PR actions from registered hooks", () => {
    const handler = vi.fn();
    win.__middleman_hooks = {
      actions: {
        pullRequest: [{ id: "pr1", label: "Test", handler }],
      },
    };
    const actions = getPullRequestActions();
    expect(actions).toHaveLength(1);
    expect(actions[0]!.id).toBe("pr1");
    expect(actions[0]!.label).toBe("Test");
  });
});

describe("lazy read without notify", () => {
  it("picks up hooks set after import without calling notify", () => {
    win.__middleman_hooks = {
      actions: {
        pullRequest: [{ id: "lazy-pr", label: "Lazy PR", handler: vi.fn() }],
        issue: [{ id: "lazy-iss", label: "Lazy Issue", handler: vi.fn() }],
      },
    };
    expect(getPullRequestActions()).toHaveLength(1);
    expect(getPullRequestActions()[0]!.id).toBe("lazy-pr");
    expect(getIssueActions()).toHaveLength(1);
    expect(getIssueActions()[0]!.id).toBe("lazy-iss");
  });

  it("picks up replaced hooks without calling notify", () => {
    win.__middleman_hooks = {
      actions: { pullRequest: [{ id: "v1", label: "V1", handler: vi.fn() }] },
    };
    win.__middleman_hooks = {
      actions: { pullRequest: [{ id: "v2", label: "V2", handler: vi.fn() }] },
    };
    expect(getPullRequestActions()[0]!.id).toBe("v2");
  });
});

describe("getIssueActions", () => {
  it("returns empty array when no hooks registered", () => {
    expect(getIssueActions()).toEqual([]);
  });

  it("returns issue actions from registered hooks", () => {
    const handler = vi.fn();
    win.__middleman_hooks = {
      actions: {
        issue: [{ id: "iss1", label: "Issue Action", handler }],
      },
    };
    const actions = getIssueActions();
    expect(actions).toHaveLength(1);
    expect(actions[0]!.id).toBe("iss1");
  });

  it("picks up in-place mutation via manual notify", () => {
    const hooks = { actions: { issue: [] as ActionHook[] } };
    win.__middleman_hooks = hooks;
    expect(getIssueActions()).toHaveLength(0);
    hooks.actions.issue.push({ id: "mut", label: "Mutated", handler: vi.fn() });
    win.__middleman_notify_hooks_changed();
    expect(getIssueActions()).toHaveLength(1);
    expect(getIssueActions()[0]!.id).toBe("mut");
  });
});

describe("invokeAction", () => {
  it("passes correct context to handler", () => {
    const handler = vi.fn();
    const action: ActionHook = { id: "a", label: "A", handler };
    invokeAction(action, { owner: "org", name: "repo", number: 42 });
    expect(handler).toHaveBeenCalledWith({
      owner: "org",
      name: "repo",
      number: 42,
    });
  });

  it("catches sync errors from handler", () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    const action: ActionHook = {
      id: "b",
      label: "B",
      handler: () => { throw new Error("boom"); },
    };
    invokeAction(action, { owner: "o", name: "n", number: 1 });
    expect(spy).toHaveBeenCalledWith(
      "Embedding action error:",
      expect.any(Error),
    );
    spy.mockRestore();
  });

  it("catches async errors from handler", async () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    const action: ActionHook = {
      id: "c",
      label: "C",
      handler: () => Promise.reject(new Error("async boom")),
    };
    invokeAction(action, { owner: "o", name: "n", number: 1 });
    await vi.waitFor(() => {
      expect(spy).toHaveBeenCalledWith(
        "Embedding action error:",
        expect.any(Error),
      );
    });
    spy.mockRestore();
  });
});
