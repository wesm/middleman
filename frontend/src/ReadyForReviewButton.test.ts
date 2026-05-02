import { cleanup, fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mockPost = vi.fn();
const mockLoadDetail = vi.fn();
const mockLoadPulls = vi.fn();

vi.mock("../../packages/ui/src/context.js", () => ({
  getClient: () => ({
    POST: mockPost,
  }),
  getStores: () => ({
    detail: {
      loadDetail: mockLoadDetail,
    },
    pulls: {
      loadPulls: mockLoadPulls,
    },
  }),
}));

import ReadyForReviewButton from "../../packages/ui/src/components/detail/ReadyForReviewButton.svelte";

describe("ReadyForReviewButton", () => {
  beforeEach(() => {
    mockPost.mockReset();
    mockLoadDetail.mockReset();
    mockLoadPulls.mockReset();
    mockLoadDetail.mockResolvedValue(undefined);
    mockLoadPulls.mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
  });

  it("refreshes detail and pull lists after marking ready for review", async () => {
    mockPost.mockResolvedValue({ data: { status: "ready_for_review" } });

    render(ReadyForReviewButton, {
      props: { owner: "wesm", name: "middleman", number: 141, size: "sm" },
    });

    await fireEvent.click(
      screen.getByRole("button", { name: /ready for review/i }),
    );

    expect(mockLoadDetail).toHaveBeenCalledWith(
      "wesm",
      "middleman",
      141,
      { platformHost: undefined },
    );
    expect(mockLoadPulls).toHaveBeenCalledTimes(1);
  });

  it("passes platform_host when marking a host-qualified PR ready", async () => {
    mockPost.mockResolvedValue({ data: { status: "ready_for_review" } });

    render(ReadyForReviewButton, {
      props: {
        owner: "wesm",
        name: "middleman",
        number: 141,
        platformHost: "ghe.example.com",
        size: "sm",
      },
    });

    await fireEvent.click(
      screen.getByRole("button", { name: /ready for review/i }),
    );

    expect(mockPost).toHaveBeenCalledWith(
      "/repos/{owner}/{name}/pulls/{number}/ready-for-review",
      {
        params: {
          path: { owner: "wesm", name: "middleman", number: 141 },
        },
        body: { platform_host: "ghe.example.com" },
      },
    );
  });

  it("refreshes stale draft state after a GitHub 404", async () => {
    mockPost.mockResolvedValue({
      error: {
        detail:
          "marking wesm/middleman#141 ready for review: POST https://api.github.com/repos/wesm/middleman/pulls/141/ready_for_review: 404 Not Found []",
      },
    });

    render(ReadyForReviewButton, {
      props: { owner: "wesm", name: "middleman", number: 141, size: "sm" },
    });

    await fireEvent.click(
      screen.getByRole("button", { name: /ready for review/i }),
    );

    expect(mockLoadDetail).toHaveBeenCalledWith(
      "wesm",
      "middleman",
      141,
      { platformHost: undefined },
    );
    expect(mockLoadPulls).toHaveBeenCalledTimes(1);
  });
});
