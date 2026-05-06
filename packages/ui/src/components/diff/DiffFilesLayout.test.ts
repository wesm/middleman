import {
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import { STORES_KEY } from "../../context.js";
import type { DiffStore } from "../../stores/diff.svelte.js";
import DiffFilesLayout from "./DiffFilesLayout.svelte";

vi.mock("./DiffToolbar.svelte", () => ({
  default: vi.fn(),
}));

vi.mock("./DiffSidebar.svelte", () => ({
  default: vi.fn(),
}));

vi.mock("./DiffView.svelte", () => ({
  default: vi.fn(),
}));

function renderLayout() {
  return render(DiffFilesLayout, {
    props: {
      owner: "acme",
      name: "widgets",
      number: 1,
    },
    context: new Map([
      [
        STORES_KEY,
        {
          diff: {} as DiffStore,
          pulls: { getSelectedPR: vi.fn() },
        },
      ],
    ]),
  });
}

describe("DiffFilesLayout", () => {
  afterEach(() => {
    cleanup();
    localStorage.removeItem("diff-file-tree-width");
  });

  it("resizes and remembers the changed-file tree width", async () => {
    renderLayout();

    const fileTree = screen.getByRole("complementary", {
      name: "Changed files",
    });
    const resizeHandle = screen.getByRole("button", {
      name: "Resize file tree",
    });

    expect(fileTree.getAttribute("style")).toContain(
      "--diff-file-tree-width: 280px",
    );

    await fireEvent.mouseDown(resizeHandle, { clientX: 280 });
    await fireEvent.mouseMove(window, { clientX: 360 });
    await fireEvent.mouseUp(window);

    expect(fileTree.getAttribute("style")).toContain(
      "--diff-file-tree-width: 360px",
    );
    expect(localStorage.getItem("diff-file-tree-width")).toBe("360");
  });

});
