import { cleanup, fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mockPost = vi.fn();
const mockLoadDetail = vi.fn();
const mockLoadPulls = vi.fn();

vi.mock("../../packages/ui/src/context.js", () => ({
  getClient: () => ({ POST: mockPost }),
  getStores: () => ({
    detail: { loadDetail: mockLoadDetail },
    pulls: { loadPulls: mockLoadPulls },
  }),
}));

import ApproveButton from "../../packages/ui/src/components/detail/ApproveButton.svelte";

describe("ApproveButton tooltips", () => {
  beforeEach(() => {
    mockPost.mockReset();
    mockLoadDetail.mockReset();
    mockLoadPulls.mockReset();
    mockPost.mockResolvedValue({});
    mockLoadDetail.mockResolvedValue(undefined);
    mockLoadPulls.mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
  });

  it("collapsed button title describes opening the form, not submitting", () => {
    render(ApproveButton, {
      props: { owner: "acme", name: "widget", number: 1 },
    });

    const trigger = screen.getByRole("button", { name: /approve/i });
    expect(trigger.getAttribute("title")).toBe(
      "Open the approval form to submit a code review on this pull request",
    );
  });

  it("expanded submit button carries the actual submit-review tooltip", async () => {
    render(ApproveButton, {
      props: { owner: "acme", name: "widget", number: 1 },
    });

    await fireEvent.click(screen.getByRole("button", { name: /approve/i }));

    const submit = screen.getByRole("button", { name: /^approve$/i });
    expect(submit.getAttribute("title")).toBe(
      "Submit an approving code review on this pull request",
    );
  });

  it("collapses and clears the draft when the PR identity changes", async () => {
    const { rerender } = render(ApproveButton, {
      props: { owner: "acme", name: "widget", number: 1 },
    });

    await fireEvent.click(screen.getByRole("button", { name: /approve/i }));
    const textarea = screen.getByRole("textbox") as HTMLTextAreaElement;
    await fireEvent.input(textarea, { target: { value: "lgtm A" } });
    expect(textarea.value).toBe("lgtm A");

    await rerender({ owner: "acme", name: "widget", number: 2 });

    expect(screen.queryByRole("textbox")).toBeNull();
    expect(
      screen.getByRole("button", { name: /approve/i }).getAttribute("title"),
    ).toBe(
      "Open the approval form to submit a code review on this pull request",
    );
  });

  it("passes platform_host when approving a host-qualified PR", async () => {
    render(ApproveButton, {
      props: {
        owner: "acme",
        name: "widget",
        number: 7,
        platformHost: "ghe.example.com",
      },
    });

    await fireEvent.click(screen.getByRole("button", { name: /approve/i }));
    await fireEvent.click(screen.getByRole("button", { name: /^approve$/i }));

    expect(mockPost).toHaveBeenCalledWith(
      "/repos/{owner}/{name}/pulls/{number}/approve",
      {
        params: {
          path: { owner: "acme", name: "widget", number: 7 },
          query: { platform_host: "ghe.example.com" },
        },
        body: { body: "" },
      },
    );
    expect(mockLoadDetail).toHaveBeenCalledWith(
      "acme",
      "widget",
      7,
      { platformHost: "ghe.example.com" },
    );
  });
});
