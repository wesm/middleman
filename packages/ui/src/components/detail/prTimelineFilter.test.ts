import { beforeEach, describe, expect, it } from "vitest";
import type { PREvent } from "../../api/types.js";
import {
  activePRTimelineFilterCount,
  DEFAULT_PR_TIMELINE_FILTER,
  filterPREvents,
  loadPRTimelineFilter,
  savePRTimelineFilter,
  timelineEventBucket,
} from "./prTimelineFilter.js";

function event(overrides: Partial<PREvent>): PREvent {
  return {
    ID: 1,
    MergeRequestID: 1,
    PlatformID: null,
    EventType: "issue_comment",
    Author: "alice",
    Summary: "",
    Body: "body",
    MetadataJSON: "",
    CreatedAt: "2024-06-01T12:00:00Z",
    DedupeKey: "event-1",
    ...overrides,
  } as PREvent;
}

describe("prTimelineFilter", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("defaults to showing every bucket and bot activity", () => {
    expect(loadPRTimelineFilter()).toEqual(DEFAULT_PR_TIMELINE_FILTER);
  });

  it("persists valid filter state to localStorage", () => {
    savePRTimelineFilter({
      showMessages: false,
      showCommitDetails: true,
      showEvents: true,
      showForcePushes: false,
      hideBots: true,
    });

    expect(loadPRTimelineFilter()).toEqual({
      showMessages: false,
      showCommitDetails: true,
      showEvents: true,
      showForcePushes: false,
      hideBots: true,
    });
  });

  it("classifies event buckets", () => {
    expect(timelineEventBucket(event({ EventType: "issue_comment" }))).toBe(
      "messages",
    );
    expect(timelineEventBucket(event({ EventType: "review" }))).toBe(
      "messages",
    );
    expect(timelineEventBucket(event({ EventType: "commit" }))).toBe(
      "commitDetails",
    );
    expect(timelineEventBucket(event({ EventType: "force_push" }))).toBe(
      "forcePushes",
    );
    expect(timelineEventBucket(event({ EventType: "cross_referenced" }))).toBe(
      "events",
    );
    expect(timelineEventBucket(event({ EventType: "renamed_title" }))).toBe(
      "events",
    );
    expect(timelineEventBucket(event({ EventType: "base_ref_changed" }))).toBe(
      "events",
    );
  });

  it("filters by disabled buckets and bots", () => {
    const events = [
      event({ ID: 1, EventType: "issue_comment", Author: "alice" }),
      event({ ID: 2, EventType: "review", Author: "renovate[bot]" }),
      event({ ID: 3, EventType: "commit", Author: "alice" }),
      event({ ID: 4, EventType: "force_push", Author: "alice" }),
      event({ ID: 5, EventType: "base_ref_changed", Author: "alice" }),
    ];

    expect(
      filterPREvents(events, {
        showMessages: true,
        showCommitDetails: false,
        showEvents: true,
        showForcePushes: false,
        hideBots: true,
      }).map((item) => item.ID),
    ).toEqual([1, 5]);
  });

  it("counts active timeline filters", () => {
    expect(activePRTimelineFilterCount(DEFAULT_PR_TIMELINE_FILTER)).toBe(0);
    expect(
      activePRTimelineFilterCount({
        showMessages: false,
        showCommitDetails: true,
        showEvents: false,
        showForcePushes: true,
        hideBots: true,
      }),
    ).toBe(3);
  });
});
