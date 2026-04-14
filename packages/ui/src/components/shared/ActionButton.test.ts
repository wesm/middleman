import { cleanup, render, screen } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";
import ActionButton from "./ActionButton.svelte";

describe("ActionButton", () => {
  afterEach(() => {
    cleanup();
  });

  it("renders a consistent shared action button contract", () => {
    render(ActionButton, {
      props: {
        tone: "danger",
        surface: "soft",
        class: "btn--close",
        label: "Close",
      },
    });

    const button = screen.getByRole("button", { name: "Close" });
    expect(button.classList.contains("action-button")).toBe(true);
    expect(button.classList.contains("action-button--danger")).toBe(true);
    expect(button.classList.contains("action-button--soft")).toBe(true);
    expect(button.classList.contains("btn--close")).toBe(true);
    expect(button.getAttribute("type")).toBe("button");
  });

  it("supports solid success buttons for primary actions", () => {
    render(ActionButton, {
      props: {
        tone: "success",
        surface: "solid",
        label: "Merge",
      },
    });

    const button = screen.getByRole("button", { name: "Merge" });
    expect(button.classList.contains("action-button--success")).toBe(true);
    expect(button.classList.contains("action-button--solid")).toBe(true);
  });
});
