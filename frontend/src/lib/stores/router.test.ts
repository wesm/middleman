import { describe, expect, it, beforeEach, afterEach, vi } from "vitest";
import {
  navigate,
  getRoute,
  getDetailTab,
  isDiffView,
  getSelectedPRFromRoute,
  getPage,
} from "./router.svelte.js";

describe("router /pulls/.../files route", () => {
  beforeEach(() => {
    // Reset to a known state.
    navigate("/pulls");
  });

  it("parses /pulls/:owner/:name/:number/files as list view with files tab", () => {
    navigate("/pulls/acme/widgets/42/files");
    const route = getRoute();
    expect(route).toEqual({
      page: "pulls",
      view: "list",
      selected: { owner: "acme", name: "widgets", number: 42 },
      tab: "files",
    });
  });

  it("getDetailTab returns files for /files route", () => {
    navigate("/pulls/acme/widgets/42/files");
    expect(getDetailTab()).toBe("files");
  });

  it("getDetailTab returns conversation for non-files PR route", () => {
    navigate("/pulls/acme/widgets/42");
    expect(getDetailTab()).toBe("conversation");
  });

  it("isDiffView returns true for /files route", () => {
    navigate("/pulls/acme/widgets/42/files");
    expect(isDiffView()).toBe(true);
  });

  it("isDiffView returns false for conversation route", () => {
    navigate("/pulls/acme/widgets/42");
    expect(isDiffView()).toBe(false);
  });

  it("isDiffView returns false for board view", () => {
    navigate("/pulls/board");
    expect(isDiffView()).toBe(false);
  });

  it("getSelectedPRFromRoute returns PR for /files route", () => {
    navigate("/pulls/acme/widgets/42/files");
    expect(getSelectedPRFromRoute()).toEqual({
      owner: "acme",
      name: "widgets",
      number: 42,
    });
  });

  it("getSelectedPRFromRoute returns PR for conversation route", () => {
    navigate("/pulls/acme/widgets/42");
    expect(getSelectedPRFromRoute()).toEqual({
      owner: "acme",
      name: "widgets",
      number: 42,
    });
  });

  it("getSelectedPRFromRoute returns null for pull list without selection", () => {
    navigate("/pulls");
    expect(getSelectedPRFromRoute()).toBeNull();
  });

  it("getPage returns pulls for /files route", () => {
    navigate("/pulls/acme/widgets/42/files");
    expect(getPage()).toBe("pulls");
  });
});

describe("router basic routes", () => {
  it("parses /design-system", () => {
    navigate("/design-system");
    expect(getRoute()).toEqual({ page: "design-system" });
    expect(getPage()).toBe("design-system");
  });

  it("parses /pulls as list view", () => {
    navigate("/pulls");
    expect(getRoute()).toEqual({ page: "pulls", view: "list" });
  });

  it("parses /pulls/board as board view", () => {
    navigate("/pulls/board");
    expect(getRoute()).toEqual({ page: "pulls", view: "board" });
  });

  it("parses /pulls/:owner/:name/:number as conversation", () => {
    navigate("/pulls/org/repo/7");
    const route = getRoute();
    expect(route).toEqual({
      page: "pulls",
      view: "list",
      selected: { owner: "org", name: "repo", number: 7 },
    });
  });

  it("parses / as activity", () => {
    navigate("/");
    expect(getRoute()).toEqual({ page: "activity" });
  });

  it("parses /repos", () => {
    navigate("/repos");
    expect(getRoute()).toEqual({ page: "repos" });
  });

  it("parses /repos/", () => {
    navigate("/repos/");
    expect(getRoute()).toEqual({ page: "repos" });
  });

  it("parses /issues/:owner/:name/:number", () => {
    navigate("/issues/org/repo/3");
    expect(getRoute()).toEqual({
      page: "issues",
      selected: { owner: "org", name: "repo", number: 3 },
    });
  });

  it("preserves issue platform_host query state", () => {
    navigate("/issues/org/repo/3?platform_host=ghe.example.com");
    expect(getRoute()).toEqual({
      page: "issues",
      selected: {
        owner: "org",
        name: "repo",
        number: 3,
        platformHost: "ghe.example.com",
      },
    });
  });

  it("treats legacy /workspaces/panel routes as the workspaces page", () => {
    navigate("/workspaces/panel/github.com/acme/widgets/42");
    expect(getRoute()).toEqual({ page: "workspaces" });
    expect(getPage()).toBe("workspaces");
  });
});

describe("router navigation events", () => {
  beforeEach(() => {
    navigate("/pulls");
  });

  afterEach(() => {
    delete (window as unknown as { __middleman_config?: unknown }).__middleman_config;
    (window as unknown as { __middleman_notify_config_changed?: () => void })
      .__middleman_notify_config_changed?.();
  });

  function installOnNavigate(spy: ReturnType<typeof vi.fn>): void {
    (window as unknown as { __middleman_config?: unknown }).__middleman_config = { onNavigate: spy };
    (window as unknown as { __middleman_notify_config_changed?: () => void })
      .__middleman_notify_config_changed?.();
  }

  it("fires onNavigate with pull payload for /files route", () => {
    const spy = vi.fn();
    installOnNavigate(spy);

    navigate("/pulls/acme/widgets/42/files");

    expect(spy).toHaveBeenCalled();
    const payload = spy.mock.calls[spy.mock.calls.length - 1]![0];
    expect(payload.type).toBe("pull");
    expect(payload.focus).toBe(false);
    expect(payload.owner).toBe("acme");
    expect(payload.name).toBe("widgets");
    expect(payload.number).toBe(42);
  });

  it("fires onNavigate with pull payload for conversation route", () => {
    const spy = vi.fn();
    installOnNavigate(spy);

    navigate("/pulls/acme/widgets/42");

    const payload = spy.mock.calls[spy.mock.calls.length - 1]![0];
    expect(payload.type).toBe("pull");
    expect(payload.owner).toBe("acme");
    expect(payload.name).toBe("widgets");
    expect(payload.number).toBe(42);
  });

  it("fires onNavigate without owner/name/number for /pulls list", () => {
    const spy = vi.fn();
    installOnNavigate(spy);

    navigate("/pulls");

    const payload = spy.mock.calls[spy.mock.calls.length - 1]![0];
    expect(payload.type).toBe("pull");
    expect(payload.owner).toBeUndefined();
    expect(payload.number).toBeUndefined();
  });

  it("maps /design-system to activity navigation events", () => {
    const spy = vi.fn();
    installOnNavigate(spy);

    navigate("/design-system");

    const payload = spy.mock.calls[spy.mock.calls.length - 1]![0];
    expect(payload.type).toBe("activity");
    expect(payload.view).toBe("/design-system");
  });
});
