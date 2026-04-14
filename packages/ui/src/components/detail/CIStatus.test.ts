import { cleanup, fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";
import CIStatus from "./CIStatus.svelte";

describe("CIStatus", () => {
  afterEach(() => {
    cleanup();
  });

  it("renders expanded CI checks when chip is clicked", async () => {
    render(CIStatus, {
      props: {
        status: "success",
        checksJSON: JSON.stringify([
          {
            name: "build",
            status: "completed",
            conclusion: "success",
            url: "https://example.com/build",
            app: "GitHub Actions",
          },
          {
            name: "test",
            status: "completed",
            conclusion: "success",
            url: "https://example.com/test",
            app: "GitHub Actions",
          },
          {
            name: "lint",
            status: "completed",
            conclusion: "success",
            url: "https://example.com/lint",
            app: "GitHub Actions",
          },
        ]),
        detailLoaded: true,
        detailSyncing: false,
      },
    });

    await fireEvent.click(
      screen.getByRole("button", { name: /CI:\s*success \(3\)/i }),
    );

    expect(screen.getByText("build")).toBeTruthy();
    expect(screen.getByText("test")).toBeTruthy();
    expect(screen.getByText("lint")).toBeTruthy();
    expect(document.querySelectorAll(".ci-check")).toHaveLength(3);
  });
});
