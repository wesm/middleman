import { cleanup, fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import Palette from "./Palette.svelte";
import {
  closePalette,
  openPalette,
  resetPaletteState,
} from "../../stores/keyboard/palette-state.svelte.js";
import {
  registerScopedActions,
  resetRegistry,
} from "../../stores/keyboard/registry.svelte.js";
import type { Action } from "../../stores/keyboard/types.js";
import { resetModalStack } from "@middleman/ui/stores/keyboard/modal-stack";

const noop = (): void => {};
const trueWhen = (): boolean => true;

function action(id: string, label = id, scope: Action["scope"] = "global"): Action {
  return {
    id,
    label,
    scope,
    binding: null,
    priority: 0,
    when: trueWhen,
    handler: noop,
  };
}

describe("Palette", () => {
  afterEach(() => {
    cleanup();
    resetPaletteState();
    resetModalStack();
    resetRegistry();
  });

  it("renders only when isPaletteOpen is true", async () => {
    const { rerender } = render(Palette, { props: {} });
    expect(screen.queryByRole("dialog")).toBeNull();
    openPalette();
    await rerender({});
    const dialog = screen.getByRole("dialog", { name: "Command palette" });
    expect(dialog).not.toBeNull();
    expect(dialog.getAttribute("aria-modal")).toBe("true");
    closePalette();
    await rerender({});
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("renders preview placeholder when no results", async () => {
    const { rerender } = render(Palette, { props: {} });
    openPalette();
    await rerender({});
    const preview = screen
      .getByRole("dialog", { name: "Command palette" })
      .querySelector(".palette-preview");
    expect(preview).not.toBeNull();
    expect(preview!.textContent ?? "").toContain(
      "Highlight a result to preview it",
    );
  });

  it("preview reflects the highlighted command when results exist", async () => {
    registerScopedActions("test", [
      action("test.first", "First Action", "view-pulls"),
      action("test.second", "Second Action", "global"),
    ]);
    const { rerender } = render(Palette, { props: {} });
    openPalette();
    await rerender({});
    const preview = screen
      .getByRole("dialog", { name: "Command palette" })
      .querySelector(".palette-preview");
    expect(preview).not.toBeNull();
    const text = preview!.textContent ?? "";
    expect(text).toContain("First Action");
    expect(text).toContain("Scope: view-pulls");
  });

  it("ArrowDown moves highlight to the next row and the preview updates", async () => {
    registerScopedActions("test", [
      action("test.first", "First Action", "view-pulls"),
      action("test.second", "Second Action", "global"),
    ]);
    const { rerender } = render(Palette, { props: {} });
    openPalette();
    await rerender({});
    const dialog = screen.getByRole("dialog", { name: "Command palette" });
    const input = dialog.querySelector(".palette-input");
    expect(input).not.toBeNull();
    await fireEvent.keyDown(input!, { key: "ArrowDown" });
    await rerender({});
    const preview = dialog.querySelector(".palette-preview");
    expect(preview).not.toBeNull();
    const text = preview!.textContent ?? "";
    expect(text).toContain("Second Action");
    expect(text).toContain("Scope: global");
  });

  it("ArrowUp at the top is a no-op", async () => {
    registerScopedActions("test", [
      action("test.first", "First Action", "view-pulls"),
      action("test.second", "Second Action", "global"),
    ]);
    const { rerender } = render(Palette, { props: {} });
    openPalette();
    await rerender({});
    const dialog = screen.getByRole("dialog", { name: "Command palette" });
    const input = dialog.querySelector(".palette-input");
    expect(input).not.toBeNull();
    await fireEvent.keyDown(input!, { key: "ArrowUp" });
    await rerender({});
    const preview = dialog.querySelector(".palette-preview");
    expect(preview).not.toBeNull();
    const text = preview!.textContent ?? "";
    expect(text).toContain("First Action");
    expect(text).toContain("Scope: view-pulls");
  });

  it("Enter runs the highlighted command's handler and closes the palette", async () => {
    let ran = false;
    registerScopedActions("test-run-enter", [
      {
        id: "test.run",
        label: "Test run",
        scope: "global",
        binding: null,
        priority: 0,
        when: trueWhen,
        handler: () => {
          ran = true;
        },
      },
    ]);
    const { rerender } = render(Palette, { props: {} });
    openPalette();
    await rerender({});
    const dialog = screen.getByRole("dialog", { name: "Command palette" });
    const input = dialog.querySelector(".palette-input");
    expect(input).not.toBeNull();
    await fireEvent.keyDown(input!, { key: "Enter" });
    await rerender({});
    expect(ran).toBe(true);
    expect(screen.queryByRole("dialog", { name: "Command palette" })).toBeNull();
  });

  it("clicking a command row runs its handler and closes the palette", async () => {
    let ran = false;
    registerScopedActions("test-run-click", [
      {
        id: "test.click",
        label: "Test click",
        scope: "global",
        binding: null,
        priority: 0,
        when: trueWhen,
        handler: () => {
          ran = true;
        },
      },
    ]);
    const { rerender } = render(Palette, { props: {} });
    openPalette();
    await rerender({});
    const dialog = screen.getByRole("dialog", { name: "Command palette" });
    const row = dialog.querySelector(".palette-row");
    expect(row).not.toBeNull();
    await fireEvent.click(row!);
    await rerender({});
    expect(ran).toBe(true);
    expect(screen.queryByRole("dialog", { name: "Command palette" })).toBeNull();
  });
});
