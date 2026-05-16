import { cleanup, fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";
import CIStatus from "./CIStatus.svelte";

describe("CIStatus", () => {
  afterEach(() => {
    cleanup();
  });

  it("shows spinners for pending checks during refresh", () => {
    render(CIStatus, {
      props: {
        status: "pending",
        checksJSON: JSON.stringify([
          {
            name: "build",
            status: "in_progress",
            conclusion: "",
            url: "",
            app: "GitHub Actions",
          },
          {
            name: "lint",
            status: "completed",
            conclusion: "success",
            url: "",
            app: "GitHub Actions",
          },
        ]),
        detailLoaded: true,
        detailSyncing: true,
        expanded: true,
      },
    });

    expect(document.querySelectorAll(".ci-check .sync-spinner")).toHaveLength(1);
    expect(screen.queryByText("◦")).toBeNull();
    expect(screen.getByText("✓")).toBeTruthy();
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
            duration_seconds: 135,
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
          {
            name: "roborev",
            status: "in_progress",
            conclusion: "",
            url: "",
            app: "roborev",
          },
        ]),
        detailLoaded: true,
        detailSyncing: false,
      },
    });

    await fireEvent.click(
      screen.getByRole("button", { name: /CI:\s*success \(4\)/i }),
    );

    expect(screen.getByText("build")).toBeTruthy();
    expect(screen.getByText("test")).toBeTruthy();
    expect(screen.getByText("lint")).toBeTruthy();
    expect(document.querySelectorAll(".ci-name")).toHaveLength(4);
    expect(document.querySelectorAll(".ci-check")).toHaveLength(4);
    expect(document.querySelectorAll("a.ci-check")).toHaveLength(3);
    expect(document.querySelector(".ci-check--static")).toBeTruthy();
    expect(screen.getByText("2m 15s")).toBeTruthy();
  });
});
