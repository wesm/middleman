import { describe, expect, it } from "vitest";

import type { PullRequest } from "../api/types.js";
import {
  classifyPR,
  groupByWorkflow,
} from "./workflow.svelte.js";

function pr(
  id: number,
  kanbanStatus: string,
  lastActivityAt: string,
  worktreeLinks: PullRequest["worktree_links"] = [],
): PullRequest {
  return {
    ID: id,
    Number: id,
    State: "open",
    KanbanStatus: kanbanStatus,
    LastActivityAt: lastActivityAt,
    worktree_links: worktreeLinks,
  } as PullRequest;
}

describe("PR status grouping", () => {
  it("classifies by kanban status instead of worktree presence", () => {
    expect(classifyPR(pr(1, "reviewing", "2026-01-01T00:00:00Z"))).toBe(
      "reviewing",
    );
    expect(
      classifyPR(
        pr(2, "waiting", "2026-01-01T00:00:00Z", [
          {
            worktree_key: "projects/example",
            worktree_branch: "example",
          },
        ]),
      ),
    ).toBe("waiting");
  });

  it("groups PRs in kanban order and falls back missing statuses to New", () => {
    const groups = groupByWorkflow([
      pr(1, "reviewing", "2026-01-01T00:00:00Z"),
      pr(2, "awaiting_merge", "2026-01-03T00:00:00Z"),
      pr(3, "", "2026-01-04T00:00:00Z"),
      pr(4, "waiting", "2026-01-02T00:00:00Z"),
    ]);

    expect(groups.map((group) => [group.group, group.label])).toEqual([
      ["new", "New"],
      ["reviewing", "Reviewing"],
      ["waiting", "Waiting"],
      ["awaiting_merge", "Awaiting Merge"],
    ]);
    expect(groups[0]?.items.map((item) => item.ID)).toEqual([3]);
  });
});
