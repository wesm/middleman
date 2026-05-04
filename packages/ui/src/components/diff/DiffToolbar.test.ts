import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";
import { STORES_KEY } from "../../context.js";
import { createDiffStore } from "../../stores/diff.svelte.js";
import DiffToolbar from "./DiffToolbar.svelte";

function renderToolbar(options: { compact?: boolean } = {}) {
  const diff = createDiffStore();
  render(DiffToolbar, {
    props: options,
    context: new Map([[STORES_KEY, { diff }]]),
  });
  return { diff };
}

describe("DiffToolbar", () => {
  afterEach(() => {
    cleanup();
    localStorage.removeItem("diff-word-wrap");
  });

  it("defaults the changed file category filter to all and renders category buttons", async () => {
    const { diff } = renderToolbar();

    expect(diff.getFileCategoryFilter()).toBe("all");
    expect(screen.queryByRole("combobox")).toBeNull();

    const labels = within(screen.getByRole("group", {
      name: "Filter changed files",
    }))
      .getAllByRole("button")
      .map((button) => button.textContent?.replace(/\s+/g, " ").trim());
    expect(labels).toEqual([
      "Plans/docs (0)",
      "Code (0)",
      "Tests (0)",
      "Other (0)",
      "All (0)",
    ]);

    expect(screen.getByRole("button", { name: "All (0)" })
      .getAttribute("aria-pressed")).toBe("true");

    await fireEvent.click(
      screen.getByRole("button", { name: "Code (0)" }),
    );

    expect(diff.getFileCategoryFilter()).toBe("code");
    expect(screen.getByRole("button", { name: "Code (0)" })
      .getAttribute("aria-pressed")).toBe("true");
  });

  it("toggles the word wrap preference", async () => {
    const { diff } = renderToolbar();
    const wordWrap = screen.getByRole("switch", { name: "Word wrap" });

    expect(diff.getWordWrap()).toBe(false);
    expect(wordWrap.getAttribute("aria-checked")).toBe("false");

    await fireEvent.click(wordWrap);

    expect(diff.getWordWrap()).toBe(true);
    expect(wordWrap.getAttribute("aria-checked")).toBe("true");
    expect(localStorage.getItem("diff-word-wrap")).toBe("true");
  });

  it("collapses diff filters behind a more button in compact mode", async () => {
    const { diff } = renderToolbar({ compact: true });

    expect(screen.getByText("All")).toBeTruthy();
    expect(screen.getByText("Tab 4")).toBeTruthy();
    expect(screen.queryByRole("group", {
      name: "Filter changed files",
    })).toBeNull();

    await fireEvent.click(
      screen.getByRole("button", { name: "More diff filters" }),
    );

    const fileFilters = screen.getByRole("group", {
      name: "Filter changed files",
    });
    await fireEvent.click(
      within(fileFilters).getByRole("button", { name: "Code (0)" }),
    );

    expect(diff.getFileCategoryFilter()).toBe("code");
    expect(screen.getAllByText("Code").length).toBeGreaterThan(0);

    const tabWidth = screen.getByRole("group", { name: "Tab width" });
    await fireEvent.click(within(tabWidth).getByRole("button", { name: "8" }));

    expect(diff.getTabWidth()).toBe(8);

    const wordWrap = screen.getByRole("switch", { name: "Word wrap" });
    await fireEvent.click(wordWrap);

    expect(diff.getWordWrap()).toBe(true);
  });
});
