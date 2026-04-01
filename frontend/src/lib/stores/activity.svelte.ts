import { apiErrorMessage, client } from "../api/runtime.js";
import type { ActivityItem, ActivityParams, ActivitySettings } from "../api/types.js";
import { getGlobalRepo, setGlobalRepo } from "./filter.svelte.js";

// --- constants ---

export type TimeRange = "24h" | "7d" | "30d" | "90d";
export type ViewMode = "flat" | "threaded";
export type ItemFilter = "all" | "prs" | "issues";

const RANGE_MS: Record<TimeRange, number> = {
  "24h": 24 * 60 * 60 * 1000,
  "7d": 7 * 24 * 60 * 60 * 1000,
  "30d": 30 * 24 * 60 * 60 * 1000,
  "90d": 90 * 24 * 60 * 60 * 1000,
};

// --- state ---

let items = $state<ActivityItem[]>([]);
let loading = $state(false);
let error = $state<string | null>(null);
let capped = $state(false);
let filterTypes = $state<string[]>([]);
let searchQuery = $state<string | undefined>(undefined);
let timeRange = $state<TimeRange>("7d");
let viewMode = $state<ViewMode>("flat");
let pollHandle: ReturnType<typeof setInterval> | null = null;
let pollInFlight = false;
let requestVersion = 0;
let pollCount = 0;
const FULL_REFRESH_EVERY = 4; // full reload every 4th poll (~60s)

let hideClosedMerged = $state(false);
let hideBots = $state(false);
let enabledEvents = $state<Set<string>>(
  new Set(["comment", "review", "commit"]),
);
let itemFilter = $state<ItemFilter>("all");
let initialized = false;

// --- reads ---

export function getActivityItems(): ActivityItem[] {
  return items;
}

export function isActivityLoading(): boolean {
  return loading;
}

export function getActivityError(): string | null {
  return error;
}

export function isActivityCapped(): boolean {
  return capped;
}

export function getActivityFilterTypes(): string[] {
  return filterTypes;
}

export function getActivitySearch(): string | undefined {
  return searchQuery;
}

export function getTimeRange(): TimeRange {
  return timeRange;
}

export function getViewMode(): ViewMode {
  return viewMode;
}

export function getHideClosedMerged(): boolean {
  return hideClosedMerged;
}

export function getHideBots(): boolean {
  return hideBots;
}

export function getEnabledEvents(): Set<string> {
  return enabledEvents;
}

export function getItemFilter(): ItemFilter {
  return itemFilter;
}

export function isInitialized(): boolean {
  return initialized;
}

// --- writes ---

export function setActivityFilterTypes(types: string[]): void {
  filterTypes = types;
}

export function setActivitySearch(q: string | undefined): void {
  searchQuery = q;
}

export function setTimeRange(range_: TimeRange): void {
  timeRange = range_;
}

export function setViewMode(mode: ViewMode): void {
  viewMode = mode;
}

export function setHideClosedMerged(v: boolean): void {
  hideClosedMerged = v;
}

export function setHideBots(v: boolean): void {
  hideBots = v;
}

export function setEnabledEvents(events: Set<string>): void {
  enabledEvents = events;
}

export function setItemFilter(f: ItemFilter): void {
  itemFilter = f;
}

// --- hydration ---

export function hydrateActivityDefaults(
  activity: ActivitySettings,
): void {
  viewMode = activity.view_mode;
  timeRange = activity.time_range;
  hideClosedMerged = activity.hide_closed;
  hideBots = activity.hide_bots;
}

/**
 * Called by ActivityFeed on mount. Always reads URL params (partial
 * override), then writes store state back to URL so missing params
 * are filled in. Safe to call on every mount because syncFromURL
 * only touches fields whose params are present in the URL.
 */
export function initializeFromMount(): void {
  syncFromURL();
  initialized = true;
  syncToURL();
}

// --- internals ---

function computeSince(): string {
  return new Date(Date.now() - RANGE_MS[timeRange]).toISOString();
}

function buildParams(): ActivityParams {
  const p: ActivityParams = { since: computeSince() };
  const repo = getGlobalRepo();
  if (repo) p.repo = repo;
  if (filterTypes.length > 0) p.types = filterTypes;
  if (searchQuery) p.search = searchQuery;
  return p;
}

/** Load the full time window from scratch. */
export async function loadActivity(): Promise<void> {
  const version = ++requestVersion;
  loading = true;
  error = null;
  try {
    const { data, error: requestError } = await client.GET("/activity", {
      params: { query: buildParams() },
    });
    if (requestError) {
      throw new Error(apiErrorMessage(requestError, "failed to load activity"));
    }
    if (version !== requestVersion) return;
    items = data?.items ?? [];
    capped = data?.capped ?? false;
  } catch (err) {
    if (version !== requestVersion) return;
    error = err instanceof Error ? err.message : String(err);
  } finally {
    if (version === requestVersion) loading = false;
  }
}

/**
 * Silent background refresh — merges updated item_state from the
 * server into the existing list without replacing it, so the user's
 * scroll depth and any items beyond the server's response cap are
 * preserved. Does not advance requestVersion so it never disrupts a
 * foreground loadActivity().
 */
async function refreshActivity(): Promise<void> {
  const versionAtStart = requestVersion;
  try {
    const { data, error: requestError } = await client.GET(
      "/activity",
      { params: { query: buildParams() } },
    );
    if (requestError || versionAtStart !== requestVersion) return;
    const fresh = data?.items ?? [];
    if (fresh.length === 0) return;
    // Only update item_state on existing items — new item insertion
    // is handled by the cursor-based poll path to avoid gaps.
    const freshById = new Map(fresh.map((it) => [it.id, it]));
    items = items.map((it) => {
      const updated = freshById.get(it.id);
      if (updated && updated.item_state !== it.item_state) {
        return { ...it, item_state: updated.item_state };
      }
      return it;
    });
  } catch {
    // silent
  }
}

/** Poll for new items since the newest displayed item. */
async function pollNewItems(): Promise<void> {
  if (pollInFlight) return;
  pollInFlight = true;
  try {
    await doPoll();
  } finally {
    pollInFlight = false;
  }
}

async function doPoll(): Promise<void> {
  // Skip polling while a foreground load is in flight to avoid
  // using a stale cursor with potentially new filter/time params.
  if (loading) return;
  pollCount++;
  if (items.length === 0) {
    await loadActivity();
    return;
  }
  // Periodically do a full refresh to pick up state changes
  // (e.g. merged/closed) on existing items.
  if (pollCount % FULL_REFRESH_EVERY === 0) {
    await refreshActivity();
    return;
  }
  const versionAtStart = requestVersion;
  try {
    const params = buildParams();
    params.after = items[0]!.cursor;
    const { data, error: requestError } = await client.GET("/activity", {
      params: { query: params },
    });
    if (requestError) {
      throw new Error(apiErrorMessage(requestError, "failed to poll activity"));
    }
    if (versionAtStart !== requestVersion) return;
    const resp = data;
    if (!resp) {
      return;
    }
    if (resp.capped) {
      // Too many new items -- full reload.
      await loadActivity();
      return;
    }
    const nextItems = resp.items ?? [];
    if (nextItems.length > 0) {
      const existingIds = new Set(items.map((it) => it.id));
      const newItems = nextItems.filter((it) => !existingIds.has(it.id));
      if (newItems.length > 0) {
        items = [...newItems, ...items];
      }
    }
  } catch {
    // Silent poll failure
  }
  if (versionAtStart !== requestVersion) return;
  // Prune items older than the current window.
  const cutoff = new Date(Date.now() - RANGE_MS[timeRange]);
  items = items.filter((it) => new Date(it.created_at) >= cutoff);
}

export function startActivityPolling(): void {
  stopActivityPolling();
  pollHandle = setInterval(() => {
    void pollNewItems();
  }, 15_000);
}

export function stopActivityPolling(): void {
  if (pollHandle !== null) {
    clearInterval(pollHandle);
    pollHandle = null;
  }
}

function deriveFiltersFromTypes(): void {
  if (filterTypes.length === 0) {
    itemFilter = "all";
    enabledEvents = new Set(["comment", "review", "commit"]);
    return;
  }
  const hasPR = filterTypes.includes("new_pr");
  const hasIssue = filterTypes.includes("new_issue");
  if (hasPR && !hasIssue) itemFilter = "prs";
  else if (hasIssue && !hasPR) itemFilter = "issues";
  else itemFilter = "all";
  enabledEvents = new Set(
    (["comment", "review", "commit"] as const).filter((t) =>
      filterTypes.includes(t),
    ),
  );
}

/** Sync URL query params -> store state (partial override). */
export function syncFromURL(): void {
  const sp = new URLSearchParams(window.location.search);
  if (sp.has("repo")) {
    setGlobalRepo(sp.get("repo") ?? undefined);
  }
  if (sp.has("types")) {
    const typesParam = sp.get("types");
    filterTypes = typesParam ? typesParam.split(",") : [];
  }
  if (sp.has("search"))
    searchQuery = sp.get("search") ?? undefined;
  if (sp.has("range")) {
    const rangeParam = sp.get("range");
    if (rangeParam && rangeParam in RANGE_MS)
      timeRange = rangeParam as TimeRange;
  }
  if (sp.has("view")) {
    const viewParam = sp.get("view");
    if (viewParam === "flat" || viewParam === "threaded")
      viewMode = viewParam;
  }
  deriveFiltersFromTypes();
}

/** Sync store state -> URL query params (replaceState). */
export function syncToURL(): void {
  const sp = new URLSearchParams(window.location.search);
  sp.delete("repo");
  if (filterTypes.length > 0) sp.set("types", filterTypes.join(","));
  else sp.delete("types");
  if (searchQuery) sp.set("search", searchQuery);
  else sp.delete("search");
  if (timeRange !== "7d") sp.set("range", timeRange);
  else sp.delete("range");
  if (viewMode !== "flat") sp.set("view", viewMode);
  else sp.delete("view");
  const qs = sp.toString();
  const base =
    (window.__BASE_PATH__ ?? "/").replace(/\/$/, "") || "";
  const url = (base || "/") + (qs ? `?${qs}` : "");
  history.replaceState(null, "", url);
}
