import { describe, expect, it } from "vitest";

import { buildContext } from "./context.svelte.js";

describe("buildContext", () => {
  it("returns the current page, route, and detail-tab snapshot", () => {
    const ctx = buildContext({
      pulls: { getSelectedPR: () => null },
      issues: { getSelectedIssue: () => null },
    });
    expect(ctx).toHaveProperty("page");
    expect(ctx).toHaveProperty("route");
    expect(ctx).toHaveProperty("selectedPR", null);
    expect(ctx).toHaveProperty("selectedIssue", null);
    expect(ctx).toHaveProperty("isDiffView");
    expect(ctx).toHaveProperty("detailTab");
  });
});
