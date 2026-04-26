import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import SelectDropdown from "./SelectDropdown.svelte";

describe("SelectDropdown", () => {
  afterEach(() => {
    cleanup();
  });

  it("renders a custom trigger instead of a native select", async () => {
    const onchange = vi.fn();

    const { container } = render(SelectDropdown, {
      props: {
        value: "reviewing",
        options: [
          { value: "new", label: "New" },
          { value: "reviewing", label: "Reviewing" },
        ],
        onchange,
      },
    });

    expect(container.querySelector("select")).toBeNull();

    await fireEvent.click(
      screen.getByRole("button", { name: "Reviewing" }),
    );

    const listbox = screen.getByRole("listbox");
    await fireEvent.click(
      within(listbox).getByRole("option", { name: "New" }),
    );

    expect(onchange).toHaveBeenCalledWith("new");
    expect(screen.queryByRole("listbox")).toBeNull();
  });
});
