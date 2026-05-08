import { cleanup, render, screen } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import Palette from "./Palette.svelte";
import {
  closePalette,
  openPalette,
  resetPaletteState,
} from "../../stores/keyboard/palette-state.svelte.js";
import { resetModalStack } from "@middleman/ui/stores/keyboard/modal-stack";

describe("Palette", () => {
  afterEach(() => {
    cleanup();
    resetPaletteState();
    resetModalStack();
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
});
