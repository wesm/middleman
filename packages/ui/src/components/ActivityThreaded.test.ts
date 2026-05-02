import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { ActivityItem } from "../api/types.js";
import ActivityThreaded from "./ActivityThreaded.svelte";

function activityItem(
  id: string,
  overrides: Partial<ActivityItem> = {},
): ActivityItem {
  return {
    id,
    cursor: id,
    activity_type: "comment",
    author: "alice",
    body_preview: "",
    created_at: "2026-04-27T12:00:00Z",
    item_number: 1,
    item_state: "open",
    item_title: "Add widget caching layer",
    item_type: "pr",
    item_url: "https://github.com/acme/widgets/pull/1",
    platform_host: "github.com",
    repo_owner: "acme",
    repo_name: "widgets-with-a-long-name",
    ...overrides,
  };
}

const groupByRepo = vi.hoisted(() => ({
  value: false,
}));

vi.mock("../context.js", () => ({
  getStores: () => ({
    grouping: {
      getGroupByRepo: () => groupByRepo.value,
    },
  }),
}));

describe("ActivityThreaded", () => {
  afterEach(() => {
    cleanup();
    groupByRepo.value = false;
  });

  it("keeps repo chip selector compatibility and applies ellipsis to an inner label", () => {
    const { container } = render(ActivityThreaded, {
      props: {
        items: [activityItem("comment")],
        onSelectItem: undefined,
      },
    });

    const chip = container.querySelector(".repo-chip.repo-tag");
    const label = chip?.querySelector(".repo-chip__label");

    expect(chip).not.toBeNull();
    expect(label?.textContent).toBe("acme/widgets-with-a-long-name");
  });
});
