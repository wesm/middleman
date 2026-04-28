import { describe, expect, it } from "vitest";
import { buildDiffSummaryKey } from "./diff-summary-key.js";

describe("buildDiffSummaryKey", () => {
  it("uses the stored diff range so merge-base changes invalidate cached summaries", () => {
    const pr = {
      UpdatedAt: "2026-04-28T12:00:00Z",
      Additions: 10,
      Deletions: 2,
    };

    const first = buildDiffSummaryKey(
      "acme",
      "widget",
      42,
      {
        platform_head_sha: "head-1",
        platform_base_sha: "base-1",
        diff_head_sha: "head-1",
        merge_base_sha: "merge-base-1",
      },
      pr,
    );
    const second = buildDiffSummaryKey(
      "acme",
      "widget",
      42,
      {
        platform_head_sha: "head-1",
        platform_base_sha: "base-2",
        diff_head_sha: "head-1",
        merge_base_sha: "merge-base-2",
      },
      pr,
    );

    expect(first).toBe("acme/widget#42#diff:merge-base-1:head-1");
    expect(second).toBe("acme/widget#42#diff:merge-base-2:head-1");
  });

  it("falls back to the platform base and head before PR stats", () => {
    const key = buildDiffSummaryKey(
      "acme",
      "widget",
      42,
      {
        platform_head_sha: "head-1",
        platform_base_sha: "base-1",
        diff_head_sha: "",
        merge_base_sha: "",
      },
      {
        UpdatedAt: "2026-04-28T12:00:00Z",
        Additions: 10,
        Deletions: 2,
      },
    );

    expect(key).toBe("acme/widget#42#platform:base-1:head-1");
  });
});
