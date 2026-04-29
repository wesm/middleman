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

  it("defaults the changed file category filter to all and lists all categories last", async () => {
    const { diff } = renderToolbar();

    expect(diff.getFileCategoryFilter()).toBe("all");

    const trigger = screen.getByRole("combobox", {
      name: "Filter changed files: All",
    });
    expect(trigger.textContent).toContain("All");

    await fireEvent.click(trigger);

    const labels = within(screen.getByRole("listbox"))
      .getAllByRole("option")
      .map((option) => option.textContent?.trim());
    expect(labels).toEqual([
      "Plans/docs",
      "Code",
      "Tests",
      "Other",
      "All",
    ]);

    await fireEvent.click(
      within(screen.getByRole("listbox"))
        .getByRole("option", { name: "Code" }),
    );

    expect(diff.getFileCategoryFilter()).toBe("code");
  });
});
