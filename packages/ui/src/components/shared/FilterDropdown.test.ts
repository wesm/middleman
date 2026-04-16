import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/svelte";
import {
  afterEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import FilterDropdown from "./FilterDropdown.svelte";

describe("FilterDropdown", () => {
  afterEach(() => {
    cleanup();
  });

  it("renders badge count and toggles multi-select items", async () => {
    const onComments = vi.fn();

    render(FilterDropdown, {
      props: {
        label: "Filters",
        badgeCount: 2,
        sections: [
          {
            title: "Event types",
            items: [
              {
                id: "comment",
                label: "Comments",
                active: true,
                color: "var(--accent-amber)",
                onSelect: onComments,
              },
            ],
          },
        ],
      },
    });

    expect(screen.getByText("2")).toBeTruthy();

    await fireEvent.click(
      screen.getByRole("button", {
        name: /filters/i,
      }),
    );

    await fireEvent.click(
      screen.getByRole("button", {
        name: /comments/i,
      }),
    );

    expect(onComments).toHaveBeenCalledTimes(1);
    expect(screen.getByText("Event types")).toBeTruthy();
  });

  it("supports single-select actions that close after selection", async () => {
    const onDone = vi.fn();

    render(FilterDropdown, {
      props: {
        label: "Status",
        detail: "Done",
        active: true,
        sections: [
          {
            items: [
              {
                id: "done",
                label: "Done",
                active: true,
                onSelect: onDone,
                closeOnSelect: true,
              },
            ],
          },
        ],
      },
    });

    expect(screen.getByText("Done")).toBeTruthy();

    await fireEvent.click(
      screen.getByRole("button", {
        name: /status/i,
      }),
    );

    const dropdown = document.querySelector(".filter-dropdown");
    expect(dropdown).toBeTruthy();

    await fireEvent.click(
      within(dropdown as HTMLElement).getByRole("button", {
        name: /done/i,
      }),
    );

    expect(onDone).toHaveBeenCalledTimes(1);
    expect(document.querySelector(".filter-dropdown")).toBeNull();
  });
});
