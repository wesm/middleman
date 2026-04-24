import { afterEach, describe, expect, it, vi } from "vitest";
import {
  isEmbedded,
  getThemeMode,
  getThemeColors,
  getThemeFonts,
  getThemeRadii,
  getUIConfig,
  getPullRequestActions,
  getIssueActions,
  invokeAction,
  getOnNavigate,
  initWorkspaceBridge,
} from "./embed-config.svelte.js";
import type { ActionHook } from "./embed-config.svelte.js";

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- test helper needs dynamic window access
const win = window as any;

afterEach(() => {
  delete win.__middleman_config;
});

describe("isEmbedded", () => {
  it("returns false when no config set", () => {
    expect(isEmbedded()).toBe(false);
  });

  it("returns true when config is set", () => {
    win.__middleman_config = {};
    win.__middleman_notify_config_changed();
    expect(isEmbedded()).toBe(true);
  });

  it("returns true for empty object", () => {
    win.__middleman_config = {};
    win.__middleman_notify_config_changed();
    expect(isEmbedded()).toBe(true);
  });
});

describe("theme config", () => {
  it("returns undefined mode when not set", () => {
    expect(getThemeMode()).toBeUndefined();
  });

  it("returns mode from config", () => {
    win.__middleman_config = { theme: { mode: "dark" } };
    win.__middleman_notify_config_changed();
    expect(getThemeMode()).toBe("dark");
  });

  it("returns partial colors", () => {
    win.__middleman_config = {
      theme: { colors: { bgPrimary: "#111" } },
    };
    win.__middleman_notify_config_changed();
    expect(getThemeColors()?.bgPrimary).toBe("#111");
  });

  it("returns fonts", () => {
    win.__middleman_config = {
      theme: { fonts: { sans: "SF Pro" } },
    };
    win.__middleman_notify_config_changed();
    expect(getThemeFonts()?.sans).toBe("SF Pro");
  });

  it("returns radii", () => {
    win.__middleman_config = {
      theme: { radii: { sm: "2px" } },
    };
    win.__middleman_notify_config_changed();
    expect(getThemeRadii()?.sm).toBe("2px");
  });
});

describe("UI config", () => {
  it("returns defaults when not set", () => {
    const ui = getUIConfig();
    expect(ui.hideSync).toBe(false);
    expect(ui.hideRepoSelector).toBe(false);
    expect(ui.hideStar).toBe(false);
    expect(ui.sidebarCollapsed).toBeUndefined();
    expect(ui.repo).toBeUndefined();
  });

  it("reads flags from config", () => {
    win.__middleman_config = {
      ui: { hideSync: true, repo: { owner: "a", name: "b" } },
    };
    win.__middleman_notify_config_changed();
    const ui = getUIConfig();
    expect(ui.hideSync).toBe(true);
    expect(ui.repo?.owner).toBe("a");
  });
});

describe("reset semantics", () => {
  it("reverts to defaults when properties removed", () => {
    win.__middleman_config = {
      theme: { mode: "dark" },
      ui: { hideSync: true },
    };
    win.__middleman_notify_config_changed();
    expect(getThemeMode()).toBe("dark");
    expect(getUIConfig().hideSync).toBe(true);

    // Remove properties and notify
    delete win.__middleman_config.theme;
    delete win.__middleman_config.ui;
    win.__middleman_notify_config_changed();
    expect(getThemeMode()).toBeUndefined();
    expect(getUIConfig().hideSync).toBe(false);
  });
});

describe("actions (migrated from hooks)", () => {
  it("returns empty arrays when no actions", () => {
    expect(getPullRequestActions()).toEqual([]);
    expect(getIssueActions()).toEqual([]);
  });

  it("returns PR actions from config", () => {
    const handler = vi.fn();
    win.__middleman_config = {
      actions: {
        pullRequest: [{ id: "pr1", label: "Test", handler }],
      },
    };
    win.__middleman_notify_config_changed();
    const actions = getPullRequestActions();
    expect(actions).toHaveLength(1);
    expect(actions[0]!.id).toBe("pr1");
  });

  it("returns issue actions from config", () => {
    const handler = vi.fn();
    win.__middleman_config = {
      actions: {
        issue: [{ id: "iss1", label: "Issue", handler }],
      },
    };
    win.__middleman_notify_config_changed();
    expect(getIssueActions()).toHaveLength(1);
  });

  it("picks up in-place mutation via notify", () => {
    const config = { actions: { issue: [] as ActionHook[] } };
    win.__middleman_config = config;
    win.__middleman_notify_config_changed();
    expect(getIssueActions()).toHaveLength(0);

    config.actions.issue.push({
      id: "mut", label: "Mutated", handler: vi.fn(),
    });
    win.__middleman_notify_config_changed();
    expect(getIssueActions()).toHaveLength(1);
  });
});

describe("invokeAction", () => {
  it("passes correct context to handler", () => {
    const handler = vi.fn();
    const action: ActionHook = { id: "a", label: "A", handler };
    invokeAction(action, { surface: "pull-detail", owner: "org", name: "repo", number: 42 });
    expect(handler).toHaveBeenCalledWith({
      surface: "pull-detail", owner: "org", name: "repo", number: 42,
    });
  });

  it("catches sync errors from handler", () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    const action: ActionHook = {
      id: "b", label: "B",
      handler: () => { throw new Error("boom"); },
    };
    invokeAction(action, { surface: "test", owner: "o", name: "n", number: 1 });
    expect(spy).toHaveBeenCalledWith(
      "Embedding action error:", expect.any(Error),
    );
    spy.mockRestore();
  });

  it("catches async errors from handler", async () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    const action: ActionHook = {
      id: "c", label: "C",
      handler: () => Promise.reject(new Error("async boom")),
    };
    invokeAction(action, { surface: "test", owner: "o", name: "n", number: 1 });
    await vi.waitFor(() => {
      expect(spy).toHaveBeenCalledWith(
        "Embedding action error:", expect.any(Error),
      );
    });
    spy.mockRestore();
  });
});

describe("onNavigate callback", () => {
  it("returns undefined when not set", () => {
    expect(getOnNavigate()).toBeUndefined();
  });

  it("returns callback from config", () => {
    const cb = vi.fn();
    win.__middleman_config = { onNavigate: cb };
    win.__middleman_notify_config_changed();
    expect(getOnNavigate()).toBe(cb);
  });

  it("reverts to undefined when removed", () => {
    const cb = vi.fn();
    win.__middleman_config = { onNavigate: cb };
    win.__middleman_notify_config_changed();
    delete win.__middleman_config.onNavigate;
    win.__middleman_notify_config_changed();
    expect(getOnNavigate()).toBeUndefined();
  });
});

describe("workspace bridge", () => {
  it("updates workspace selection in config", () => {
    win.__middleman_config = {
      workspace: {
        hosts: [],
        selectedHostKey: null,
        selectedWorktreeKey: null,
      },
    };
    initWorkspaceBridge();

    win.__middleman_update_selection({
      hostKey: "host-1",
      worktreeKey: "worktree-1",
    });

    expect(win.__middleman_config.workspace).toEqual({
      hosts: [],
      selectedHostKey: "host-1",
      selectedWorktreeKey: "worktree-1",
    });
  });
});
