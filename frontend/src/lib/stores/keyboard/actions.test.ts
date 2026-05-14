import { describe, expect, it } from "vitest";

import { defaultActions } from "./actions.js";
import type { Context } from "./types.js";

function ctx(page: Context["page"]): Context {
  return {
    page,
    route: { page } as never,
    selectedPR: null,
    selectedIssue: null,
    isDiffView: false,
    detailTab: "conversation",
  };
}

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

  it("cheatsheet.open binds ? with shift so the dispatcher matches the real keystroke", () => {
    // `?` is Shift+/ on a US keyboard. The dispatcher's matcher treats
    // omitted `shift` as `false`, so without an explicit `shift: true`
    // a real `?` press (event.shiftKey === true) would never fire the
    // action — Playwright's keyboard.press synthesizes the char and hides
    // this in e2e tests.
    const cheatsheet = defaultActions.find((a) => a.id === "cheatsheet.open");
    expect(cheatsheet).toBeDefined();
    expect(cheatsheet!.binding).toEqual({ key: "?", shift: true });
  });

  it("cheatsheet.open does not fire on the reviews page (roborev owns ?)", () => {
    // Roborev's ReviewsView has its own window-level `?` handler that
    // opens a help modal. If middleman's cheatsheet also fires on `?`,
    // both modals open and the cheatsheet's filter input steals focus,
    // causing roborev's Escape handler to short-circuit on its
    // tag === "INPUT" guard. Gate the action by page to avoid that.
    const cheatsheet = defaultActions.find((a) => a.id === "cheatsheet.open");
    expect(cheatsheet).toBeDefined();
    expect(cheatsheet!.when(ctx("reviews"))).toBe(false);
    expect(cheatsheet!.when(ctx("pulls"))).toBe(true);
    expect(cheatsheet!.when(ctx("issues"))).toBe(true);
  });
});
