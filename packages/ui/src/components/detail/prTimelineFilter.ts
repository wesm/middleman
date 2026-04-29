import type { PREvent } from "../../api/types.js";

export interface PRTimelineFilterState {
  showMessages: boolean;
  showCommitDetails: boolean;
  showEvents: boolean;
  showForcePushes: boolean;
  hideBots: boolean;
}

export type PRTimelineEventBucket =
  | "messages"
  | "commitDetails"
  | "events"
  | "forcePushes";

export const PR_TIMELINE_FILTER_STORAGE_KEY = "middleman-pr-timeline-filter";

export const DEFAULT_PR_TIMELINE_FILTER: PRTimelineFilterState = {
  showMessages: true,
  showCommitDetails: true,
  showEvents: true,
  showForcePushes: true,
  hideBots: false,
};

const BOT_SUFFIXES = ["[bot]", "-bot", "bot"];

export function isBotAuthor(author: string): boolean {
  const lower = author.toLowerCase();
  return BOT_SUFFIXES.some((suffix) => lower.endsWith(suffix));
}

export function timelineEventBucket(event: PREvent): PRTimelineEventBucket {
  switch (event.EventType) {
    case "issue_comment":
    case "review":
    case "review_comment":
      return "messages";
    case "commit":
      return "commitDetails";
    case "force_push":
      return "forcePushes";
    default:
      return "events";
  }
}

function normalizeFilter(
  value: Partial<PRTimelineFilterState> | null,
): PRTimelineFilterState {
  return {
    showMessages:
      value?.showMessages ?? DEFAULT_PR_TIMELINE_FILTER.showMessages,
    showCommitDetails:
      value?.showCommitDetails ?? DEFAULT_PR_TIMELINE_FILTER.showCommitDetails,
    showEvents: value?.showEvents ?? DEFAULT_PR_TIMELINE_FILTER.showEvents,
    showForcePushes:
      value?.showForcePushes ?? DEFAULT_PR_TIMELINE_FILTER.showForcePushes,
    hideBots: value?.hideBots ?? DEFAULT_PR_TIMELINE_FILTER.hideBots,
  };
}

export function loadPRTimelineFilter(): PRTimelineFilterState {
  try {
    const raw = localStorage.getItem(PR_TIMELINE_FILTER_STORAGE_KEY);
    if (!raw) return DEFAULT_PR_TIMELINE_FILTER;
    const parsed = JSON.parse(raw) as Partial<PRTimelineFilterState>;
    return normalizeFilter(parsed);
  } catch {
    return DEFAULT_PR_TIMELINE_FILTER;
  }
}

export function savePRTimelineFilter(filter: PRTimelineFilterState): void {
  try {
    localStorage.setItem(
      PR_TIMELINE_FILTER_STORAGE_KEY,
      JSON.stringify(filter),
    );
  } catch {
    // localStorage can be unavailable in private browsing or embedded contexts.
  }
}

export function filterPREvents(
  events: PREvent[],
  filter: PRTimelineFilterState,
): PREvent[] {
  return events.filter((event) => {
    if (filter.hideBots && isBotAuthor(event.Author)) return false;
    switch (timelineEventBucket(event)) {
      case "messages":
        return filter.showMessages;
      case "commitDetails":
        return filter.showCommitDetails;
      case "events":
        return filter.showEvents;
      case "forcePushes":
        return filter.showForcePushes;
    }
  });
}

export function activePRTimelineFilterCount(
  filter: PRTimelineFilterState,
): number {
  return [
    !filter.showMessages,
    !filter.showCommitDetails,
    !filter.showEvents,
    !filter.showForcePushes,
    filter.hideBots,
  ].filter(Boolean).length;
}
