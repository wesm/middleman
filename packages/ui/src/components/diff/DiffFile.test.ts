import { cleanup, fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";

// Mock highlight utils to avoid loading Shiki in tests.
vi.mock("../../utils/highlight.js", () => ({
  tokenizeLineDual: () => Promise.resolve([]),
  langFromPath: () => "text",
}));

// jsdom does not ship IntersectionObserver; stub it so DiffFile's onMount
// observer setup does not throw during render.
beforeAll(() => {
  class IntersectionObserverStub {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
    takeRecords(): IntersectionObserverEntry[] { return []; }
    root: Element | null = null;
    rootMargin = "";
    thresholds: readonly number[] = [];
  }
  (globalThis as { IntersectionObserver?: unknown }).IntersectionObserver =
    IntersectionObserverStub;
});

import DiffFile from "./DiffFile.svelte";
import type { DiffFile as DiffFileType } from "../../api/types.js";

function makeFile(overrides: Partial<DiffFileType> = {}): DiffFileType {
  return {
    path: "src/foo.ts",
    old_path: "src/foo.ts",
    status: "modified",
    is_binary: false,
    is_whitespace_only: false,
    additions: 3,
    deletions: 1,
    hunks: [{
      old_start: 1,
      old_count: 3,
      new_start: 1,
      new_count: 5,
      lines: [
        { type: "context", content: "line 1", old_num: 1, new_num: 1 },
        { type: "delete", content: "old line", old_num: 2 },
        { type: "add", content: "new line", new_num: 2 },
      ],
    }],
    ...overrides,
  };
}

// Use unique owner per test so module-level collapsed state doesn't leak.
let testCounter = 0;
function uniqueOwner(): string {
  return `test-owner-${++testCounter}`;
}

describe("DiffFile", () => {
  afterEach(() => {
    cleanup();
  });

  it("renders file content when not collapsed", () => {
    render(DiffFile, {
      props: { file: makeFile(), owner: uniqueOwner(), name: "n", number: 1 },
    });

    expect(screen.getByText("src/foo.ts")).toBeTruthy();
    expect(screen.getByText(/@@ -1,3 \+1,5 @@/)).toBeTruthy();
  });

  it("hides content after clicking the header to collapse", async () => {
    render(DiffFile, {
      props: { file: makeFile(), owner: uniqueOwner(), name: "n", number: 1 },
    });

    const header = screen.getByTitle("Collapse file");
    await fireEvent.click(header);

    const content = document.querySelector(".file-content");
    expect(content?.classList.contains("file-content--collapsed")).toBe(true);
  });

  it("shows content again after toggling collapse twice", async () => {
    render(DiffFile, {
      props: { file: makeFile(), owner: uniqueOwner(), name: "n", number: 1 },
    });

    const header = screen.getByTitle("Collapse file");
    await fireEvent.click(header);

    const expandHeader = screen.getByTitle("Expand file");
    await fireEvent.click(expandHeader);

    const content = document.querySelector(".file-content");
    expect(content?.classList.contains("file-content--collapsed")).toBe(false);
  });
});
