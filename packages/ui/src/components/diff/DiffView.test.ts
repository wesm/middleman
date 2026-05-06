import {
  cleanup,
  fireEvent,
  render,
} from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  DiffFile,
  DiffResult,
  FilesResult,
} from "../../api/types.js";
import { STORES_KEY } from "../../context.js";
import type { DiffStore } from "../../stores/diff.svelte.js";

vi.mock("./DiffFile.svelte", async () => ({
  default: (await import("./DiffViewTestFile.svelte")).default,
}));

import DiffView from "./DiffView.svelte";

if (!globalThis.CSS) {
  globalThis.CSS = {} as typeof CSS;
}
globalThis.CSS.escape ??= (value: string) => value.replace(/"/g, '\\"');

function makeFile(path: string): DiffFile {
  return {
    path,
    old_path: path,
    status: "modified",
    is_binary: false,
    is_whitespace_only: false,
    additions: 1,
    deletions: 1,
    hunks: [],
  };
}

function makeDiffStore(
  overrides: Partial<DiffStore> = {},
): DiffStore {
  const activeFile = overrides.getActiveFile?.() ?? "a.ts";
  const diff: DiffResult = {
    stale: false,
    whitespace_only_count: 0,
    files: [makeFile(activeFile)],
  };
  const fileList: FilesResult = {
    stale: false,
    files: [makeFile("a.ts"), makeFile("b.ts")],
  };

  return {
    getDiff: () => diff,
    getVisibleDiffFiles: () => diff.files,
    getVisibleFileList: () => fileList,
    isDiffLoading: () => false,
    getDiffError: () => null,
    getTabWidth: () => 4,
    getWordWrap: () => false,
    getRichPreview: () => false,
    getFilePreviewGeneration: () => 0,
    getScrollTarget: () => null,
    consumeScrollTarget: vi.fn(),
    clearScrolling: vi.fn(),
    isScrolling: () => false,
    isFileCollapsed: () => false,
    toggleFileCollapsed: vi.fn(),
    setActiveFile: vi.fn(),
    getActiveFile: () => activeFile,
    requestScrollToFile: vi.fn(),
    stepPrev: vi.fn(),
    stepNext: vi.fn(),
    loadDiff: vi.fn(),
    clearDiff: vi.fn(),
    ...overrides,
  } as unknown as DiffStore;
}

function renderDiffView(diff: DiffStore) {
  return render(DiffView, {
    props: {
      owner: "acme",
      name: "widgets",
      number: 1,
      loadOnMount: false,
    },
    context: new Map([[STORES_KEY, { diff }]]),
  });
}

describe("DiffView", () => {
  afterEach(() => {
    cleanup();
  });

  it("uses the workspace file list for keyboard navigation", async () => {
    const requestScrollToFile = vi.fn();
    const diff = makeDiffStore({ requestScrollToFile });

    renderDiffView(diff);
    await fireEvent.keyDown(window, { key: "j" });

    expect(requestScrollToFile).toHaveBeenCalledWith("b.ts");
  });

  it("keeps a scroll target pending until the file is rendered", async () => {
    const consumeScrollTarget = vi.fn();
    const diff = makeDiffStore({
      getScrollTarget: () => "b.ts",
      consumeScrollTarget,
    });

    renderDiffView(diff);
    await Promise.resolve();

    expect(consumeScrollTarget).not.toHaveBeenCalled();
  });
});
