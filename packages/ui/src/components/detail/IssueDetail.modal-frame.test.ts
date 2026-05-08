import { cleanup, render } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import IssueDetailModalFrameHarness from "./IssueDetailModalFrameHarness.test.svelte";
import {
  getStackDepth,
  getTopFrame,
  resetModalStack,
} from "../../stores/keyboard/modal-stack.svelte.js";

// Mounting the real IssueDetail requires the full Provider context
// (issues/activity stores, API client, navigate, actions, ui config).
// The harness mirrors the exact $effect pattern IssueDetail uses for
// its branch-conflict sub-modal so we can verify gated push/pop without
// rebuilding all of that scaffolding here.

describe("IssueDetail confirm sub-modal frame integration", () => {
  beforeEach(() => {
    resetModalStack();
  });

  afterEach(() => {
    cleanup();
    resetModalStack();
  });

  it("pushes a frame only while the sub-modal is open", async () => {
    expect(getStackDepth()).toBe(0);

    const { rerender, unmount } = render(IssueDetailModalFrameHarness, {
      props: { open: false },
    });
    expect(getStackDepth()).toBe(0);

    await rerender({ open: true });
    expect(getStackDepth()).toBe(1);
    expect(getTopFrame()?.frameId).toBe("issue-detail-confirm");

    await rerender({ open: false });
    expect(getStackDepth()).toBe(0);

    await rerender({ open: true });
    expect(getStackDepth()).toBe(1);

    unmount();
    expect(getStackDepth()).toBe(0);
  });
});
