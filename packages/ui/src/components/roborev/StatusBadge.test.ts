import { cleanup, render, screen } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import StatusBadge from "./StatusBadge.svelte";

describe("StatusBadge", () => {
  afterEach(() => {
    cleanup();
  });

  it("renders review status through the shared dotted chip semantics", () => {
    render(StatusBadge, {
      props: {
        status: "running",
      },
    });

    const chip = screen.getByText("running").closest(".chip");
    expect(chip).not.toBeNull();
    expect(chip?.classList.contains("chip--tone-info")).toBe(true);
    expect(chip?.querySelector(".chip__dot")).not.toBeNull();
    expect(chip?.querySelector(".status-badge")).toBeNull();
  });
});
