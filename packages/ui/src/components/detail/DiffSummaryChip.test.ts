import { cleanup, fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { DiffFile } from "../../api/types.js";
import DiffSummaryChip from "./DiffSummaryChip.svelte";

function file(
  path: string,
  additions: number,
  deletions: number,
): DiffFile {
  return {
    path,
    old_path: path,
    status: "modified",
    is_binary: false,
    is_whitespace_only: false,
    additions,
    deletions,
    hunks: [],
  };
}

describe("DiffSummaryChip", () => {
  afterEach(() => {
    cleanup();
  });

  it("loads file totals on hover and shows them by category", async () => {
    const loadFiles = vi.fn(async () => [
      file("docs/plan.md", 10, 2),
      file("src/App.svelte", 40, 6),
      file("src/App.test.ts", 20, 8),
      file("bun.lock", 1, 1),
    ]);

    render(DiffSummaryChip, {
      props: {
        additions: 71,
        deletions: 17,
        loadFiles,
      },
    });

    await fireEvent.mouseEnter(
      screen.getByRole("button", { name: /\+71\/-17/i }),
    );

    expect(await screen.findByText("Plans/docs")).toBeTruthy();
    expect(screen.queryByText("Total")).toBeNull();
    expect(screen.getByText("+10 / -2")).toBeTruthy();
    expect(screen.getByText("Code")).toBeTruthy();
    expect(screen.getByText("+40 / -6")).toBeTruthy();
    expect(screen.getByText("Tests")).toBeTruthy();
    expect(screen.getByText("+20 / -8")).toBeTruthy();
    expect(screen.getByText("Other")).toBeTruthy();
    expect(screen.getByText("+1 / -1")).toBeTruthy();
    expect(loadFiles).toHaveBeenCalledTimes(1);
  });
});
