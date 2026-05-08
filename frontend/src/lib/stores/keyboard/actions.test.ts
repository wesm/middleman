import { describe, expect, it } from "vitest";

import { defaultActions } from "./actions.js";

describe("defaultActions", () => {
  it("includes the migrated globals", () => {
    const ids = defaultActions.map((a) => a.id);
    expect(ids).toEqual(
      expect.arrayContaining([
        "go.next",
        "go.prev",
        "tab.toggle",
        "escape.list",
        "nav.pulls.list",
        "nav.pulls.board",
        "sidebar.toggle",
        "palette.open",
        "cheatsheet.open",
        "sync.repos",
        "theme.toggle",
        "nav.settings",
        "nav.repos",
        "nav.reviews",
        "nav.workspaces",
        "nav.design-system",
      ]),
    );
  });

  it("palette.open binds Cmd/Ctrl+K and Cmd/Ctrl+P", () => {
    const palette = defaultActions.find((a) => a.id === "palette.open");
    expect(palette).toBeDefined();
    expect(palette!.binding).toEqual([
      { key: "k", ctrlOrMeta: true },
      { key: "p", ctrlOrMeta: true },
    ]);
  });
});
