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

    expect(mockLoadDetail).toHaveBeenCalledWith("wesm", "middleman", 141);
    expect(mockLoadPulls).toHaveBeenCalledTimes(1);
  });
});
