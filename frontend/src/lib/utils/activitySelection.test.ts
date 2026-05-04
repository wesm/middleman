import { describe, expect, it } from "vitest";
import {
  activitySelectionToRoute,
  buildActivitySelectionSearch,
  parseActivitySelection,
  type ActivitySelection,
} from "./activitySelection";

describe("activity selection URL state", () => {
  it("parses PR conversation selection", () => {
    expect(parseActivitySelection("?selected=pr:acme/widgets/1")).toEqual({
      itemType: "pr",
      owner: "acme",
      name: "widgets",
      number: 1,
      detailTab: "conversation",
    });
  });

  it("parses canonical percent-encoded selection", () => {
    expect(parseActivitySelection("?selected=pr%3Aacme%2Fwidgets%2F1")).toEqual({
      itemType: "pr",
      owner: "acme",
      name: "widgets",
      number: 1,
      detailTab: "conversation",
    });
  });

  it("parses search strings without a leading question mark", () => {
    expect(parseActivitySelection("selected=issue:acme/widgets/10")).toEqual({
      itemType: "issue",
      owner: "acme",
      name: "widgets",
      number: 10,
      detailTab: "conversation",
    });
  });

  it("parses PR files selection", () => {
    expect(parseActivitySelection("?selected=pr:acme/widgets/1&selected_tab=files")).toEqual({
      itemType: "pr",
      owner: "acme",
      name: "widgets",
      number: 1,
      detailTab: "files",
    });
  });

  it("parses issue platform host", () => {
    expect(parseActivitySelection("?selected=issue:acme/widgets/10&platform_host=ghe.example.com")).toEqual({
      itemType: "issue",
      owner: "acme",
      name: "widgets",
      number: 10,
      platformHost: "ghe.example.com",
      detailTab: "conversation",
    });
  });

  it("preserves existing Activity filters when writing selection", () => {
    const next = buildActivitySelectionSearch("?range=30d&view=threaded", {
      itemType: "pr",
      owner: "acme",
      name: "widgets",
      number: 1,
      detailTab: "files",
    });

    expect(next.toString()).toBe(
      "range=30d&view=threaded&selected=pr%3Aacme%2Fwidgets%2F1&selected_tab=files",
    );
  });

  it("clears selection-only params without clearing filters", () => {
    const next = buildActivitySelectionSearch(
      "?range=30d&selected=issue:acme/widgets/10&platform_host=ghe.example.com",
      null,
    );

    expect(next.toString()).toBe("range=30d");
  });

  it("overwrites one PR selection with another and drops a stale files tab", () => {
    const next = buildActivitySelectionSearch(
      "?selected=pr:acme/widgets/1&selected_tab=files&range=7d",
      {
        itemType: "pr",
        owner: "octo",
        name: "tools",
        number: 2,
        detailTab: "conversation",
      },
    );

    expect(next.get("selected")).toBe("pr:octo/tools/2");
    expect(next.get("range")).toBe("7d");
    expect(next.has("selected_tab")).toBe(false);
  });

  it("overwrites an issue selection with a PR and drops platform host", () => {
    const next = buildActivitySelectionSearch(
      "?selected=issue:acme/widgets/10&platform_host=ghe.example.com&search=bug",
      {
        itemType: "pr",
        owner: "acme",
        name: "widgets",
        number: 1,
        detailTab: "conversation",
      },
    );

    expect(next.get("selected")).toBe("pr:acme/widgets/1");
    expect(next.get("search")).toBe("bug");
    expect(next.has("platform_host")).toBe(false);
  });

  it("builds destination routes for matching tabs", () => {
    const pr: ActivitySelection = {
      itemType: "pr",
      owner: "acme",
      name: "widgets",
      number: 1,
      detailTab: "files",
    };

    expect(activitySelectionToRoute(pr, "pulls")).toBe(
      "/pulls/detail/files?provider=github&platform_host=github.com&repo_path=acme%2Fwidgets&number=1",
    );
    expect(activitySelectionToRoute(pr, "issues")).toBeNull();
  });

  it("builds issue destination routes with platform host", () => {
    const issue: ActivitySelection = {
      itemType: "issue",
      owner: "acme",
      name: "widgets",
      number: 10,
      platformHost: "ghe.example.com",
      detailTab: "conversation",
    };

    expect(activitySelectionToRoute(issue, "issues")).toBe(
      "/issues/detail?provider=github&platform_host=ghe.example.com&repo_path=acme%2Fwidgets&number=10",
    );
    expect(activitySelectionToRoute(issue, "pulls")).toBeNull();
  });

  it.each([
    "",
    "?selected=garbage",
    "?selected=pr:foo",
    "?selected=foo:acme/widgets/1",
    "?selected_tab=files",
  ])("returns null for invalid selection %s", (search) => {
    expect(parseActivitySelection(search)).toBeNull();
  });
});
