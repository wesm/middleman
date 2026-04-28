import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { DiffFile } from "../../api/types.js";
import DiffSummaryChip from "./DiffSummaryChip.svelte";
import { DiffSummaryFilesResult } from "./diff-summary.js";

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
        loadFiles: async () =>
          new DiffSummaryFilesResult(false, await loadFiles()),
      },
    });

    await fireEvent.mouseEnter(
      screen.getByRole("button", { name: /\+71\/-17/i }),
    );

    expect(await screen.findByText("Plans/docs")).toBeTruthy();
    expect(screen.queryByText("Total")).toBeNull();
    expect(screen.getByText("+10/-2")).toBeTruthy();
    expect(screen.getByText("Code")).toBeTruthy();
    expect(screen.getByText("+40/-6")).toBeTruthy();
    expect(screen.getByText("Tests")).toBeTruthy();
    expect(screen.getByText("+20/-8")).toBeTruthy();
    expect(screen.getByText("Other")).toBeTruthy();
    expect(screen.getByText("+1/-1")).toBeTruthy();
    expect(loadFiles).toHaveBeenCalledTimes(1);
  });

  it("hides categories with no changed lines", async () => {
    render(DiffSummaryChip, {
      props: {
        additions: 60,
        deletions: 14,
        loadFiles: vi.fn(async () =>
          new DiffSummaryFilesResult(
            false,
            [
              file("src/App.svelte", 40, 6),
              file("src/App.test.ts", 20, 8),
            ],
          )),
      },
    });

    await fireEvent.mouseEnter(
      screen.getByRole("button", { name: /\+60\/-14/i }),
    );

    expect(await screen.findByText("Code")).toBeTruthy();
    expect(screen.getByText("+40/-6")).toBeTruthy();
    expect(screen.getByText("Tests")).toBeTruthy();
    expect(screen.getByText("+20/-8")).toBeTruthy();
    expect(screen.queryByText("Plans/docs")).toBeNull();
    expect(screen.queryByText("Other")).toBeNull();
  });

  it("does not cache stale file responses", async () => {
    const loadFiles = vi
      .fn()
      .mockResolvedValueOnce(new DiffSummaryFilesResult(true, []))
      .mockResolvedValueOnce(
        new DiffSummaryFilesResult(false, [file("src/App.svelte", 4, 1)]),
      );

    render(DiffSummaryChip, {
      props: {
        additions: 4,
        deletions: 1,
        loadFiles,
      },
    });

    const trigger = screen.getByRole("button", { name: /\+4\/-1/i });
    await fireEvent.mouseEnter(trigger);

    expect(await screen.findByText("Changed files are still refreshing."))
      .toBeTruthy();
    await fireEvent.mouseLeave(trigger);
    await fireEvent.mouseEnter(trigger);

    const popover = await screen.findByRole("status");
    expect(within(popover).getByText("Code")).toBeTruthy();
    expect(within(popover).getByText("+4/-1")).toBeTruthy();
    expect(loadFiles).toHaveBeenCalledTimes(2);
  });

  it("discards file responses for superseded summary keys", async () => {
    let resolveFirst: ((value: DiffSummaryFilesResult) => void) | undefined;
    let resolveSecond: ((value: DiffSummaryFilesResult) => void) | undefined;
    const loadFiles = vi
      .fn()
      .mockReturnValueOnce(
        new Promise<DiffSummaryFilesResult>((resolve) => {
          resolveFirst = resolve;
        }),
      )
      .mockReturnValueOnce(
        new Promise<DiffSummaryFilesResult>((resolve) => {
          resolveSecond = resolve;
        }),
      );

    const { rerender } = render(DiffSummaryChip, {
      props: {
        additions: 10,
        deletions: 0,
        summaryKey: "sha-1",
        loadFiles,
      },
    });

    await fireEvent.mouseEnter(
      screen.getByRole("button", { name: /\+10\/-0/i }),
    );
    await rerender({
      additions: 5,
      deletions: 1,
      summaryKey: "sha-2",
      loadFiles,
    });

    resolveFirst?.(
      new DiffSummaryFilesResult(false, [file("docs/old.md", 10, 0)]),
    );
    await waitFor(() => expect(loadFiles).toHaveBeenCalledTimes(2));
    resolveSecond?.(
      new DiffSummaryFilesResult(false, [file("src/new.ts", 5, 1)]),
    );

    const popover = await screen.findByRole("status");
    expect(within(popover).getByText("Code")).toBeTruthy();
    expect(within(popover).getByText("+5/-1")).toBeTruthy();
    expect(screen.queryByText("Plans/docs")).toBeNull();
  });

  it("reloads immediately when the summary key changes while open", async () => {
    const loadFiles = vi
      .fn()
      .mockResolvedValueOnce(
        new DiffSummaryFilesResult(false, [file("docs/old.md", 10, 0)]),
      )
      .mockResolvedValueOnce(
        new DiffSummaryFilesResult(false, [file("src/new.ts", 5, 1)]),
      );

    const { rerender } = render(DiffSummaryChip, {
      props: {
        additions: 10,
        deletions: 0,
        summaryKey: "sha-1",
        loadFiles,
      },
    });

    await fireEvent.mouseEnter(
      screen.getByRole("button", { name: /\+10\/-0/i }),
    );
    expect(await screen.findByText("Plans/docs")).toBeTruthy();

    await rerender({
      additions: 5,
      deletions: 1,
      summaryKey: "sha-2",
      loadFiles,
    });

    const popover = await screen.findByRole("status");
    expect(within(popover).getByText("Code")).toBeTruthy();
    expect(within(popover).getByText("+5/-1")).toBeTruthy();
    await waitFor(() => expect(loadFiles).toHaveBeenCalledTimes(2));
    expect(screen.queryByText("Plans/docs")).toBeNull();
  });
});
