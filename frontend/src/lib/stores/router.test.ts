import { describe, expect, it, beforeEach } from "vitest";
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

  it("parses /issues/:owner/:name/:number", () => {
    navigate("/issues/org/repo/3");
    expect(getRoute()).toEqual({
      page: "issues",
      selected: { owner: "org", name: "repo", number: 3 },
    });
  });
});
