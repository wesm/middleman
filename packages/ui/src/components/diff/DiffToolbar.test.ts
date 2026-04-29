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

function renderToolbar() {
  const diff = createDiffStore();
  render(DiffToolbar, {
    context: new Map([[STORES_KEY, { diff }]]),
  });
  return { diff };
}

describe("DiffToolbar", () => {
  afterEach(() => {
    cleanup();
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
});
