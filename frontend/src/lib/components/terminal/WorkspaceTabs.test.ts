import { cleanup, fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";

import WorkspaceTabs from "./WorkspaceTabs.svelte";

describe("WorkspaceTabs", () => {
  afterEach(() => cleanup());

  it("renders the workspace diff tab", async () => {
    const onSelectDiff = vi.fn();

    render(WorkspaceTabs, {
      props: {
        activeKey: "home",
        sessions: [],
        onSelectDiff,
      },
    });

    const tab = screen.getByRole("tab", { name: "Diff" });
    expect(tab.getAttribute("aria-selected")).toBe("false");

    await fireEvent.click(tab);

    expect(onSelectDiff).toHaveBeenCalledOnce();
  });
});
