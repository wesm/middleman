import { cleanup, fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("../../packages/ui/src/context.js", () => ({
  getClient: () => ({ POST: vi.fn() }),
  getStores: () => ({
    detail: { loadDetail: vi.fn() },
    pulls: { loadPulls: vi.fn() },
  }),
}));

import ApproveButton from "../../packages/ui/src/components/detail/ApproveButton.svelte";

describe("ApproveButton tooltips", () => {
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
});
