import { listActivity } from "../api/activity.js";
import type { ActivityItem, ActivityParams } from "../api/activity.js";

// --- constants ---

export type TimeRange = "24h" | "7d" | "30d" | "90d";
export type ViewMode = "flat" | "threaded";

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
let filterRepo = $state<string | undefined>(undefined);
let filterTypes = $state<string[]>([]);
let searchQuery = $state<string | undefined>(undefined);
let timeRange = $state<TimeRange>("7d");
let viewMode = $state<ViewMode>("flat");
let pollHandle: ReturnType<typeof setInterval> | null = null;
let requestVersion = 0;

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

export function getActivityFilterRepo(): string | undefined {
  return filterRepo;
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

// --- writes ---

export function setActivityFilterRepo(repo: string | undefined): void {
  filterRepo = repo;
}

export function setActivityFilterTypes(types: string[]): void {
  filterTypes = types;
}

export function setActivitySearch(q: string | undefined): void {
  searchQuery = q;
}

export function setTimeRange(range: TimeRange): void {
  timeRange = range;
}

export function setViewMode(mode: ViewMode): void {
  viewMode = mode;
}

function computeSince(): string {
  return new Date(Date.now() - RANGE_MS[timeRange]).toISOString();
}

function buildParams(): ActivityParams {
  const p: ActivityParams = { since: computeSince() };
  if (filterRepo) p.repo = filterRepo;
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
    const resp = await listActivity(buildParams());
    if (version !== requestVersion) return;
    items = resp.items;
    capped = resp.capped;
  } catch (err) {
    if (version !== requestVersion) return;
    error = err instanceof Error ? err.message : String(err);
  } finally {
    if (version === requestVersion) loading = false;
  }
}

/** Poll for new items since the newest displayed item. */
async function pollNewItems(): Promise<void> {
  if (items.length === 0) {
    await loadActivity();
    return;
  }
  try {
    const params = buildParams();
    params.after = items[0]!.cursor;
    const resp = await listActivity(params);
    if (resp.capped) {
      // Too many new items — full reload.
      await loadActivity();
      return;
    }
    if (resp.items.length > 0) {
      const existingIds = new Set(items.map((it) => it.id));
      const newItems = resp.items.filter((it) => !existingIds.has(it.id));
      if (newItems.length > 0) {
        items = [...newItems, ...items];
      }
    }
  } catch {
    // Silent poll failure
  }
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

/** Sync URL query params → store state. Called on mount. */
export function syncFromURL(): void {
  const sp = new URLSearchParams(window.location.search);
  filterRepo = sp.get("repo") ?? undefined;
  const typesParam = sp.get("types");
  filterTypes = typesParam ? typesParam.split(",") : [];
  searchQuery = sp.get("search") ?? undefined;
  const rangeParam = sp.get("range");
  if (rangeParam && rangeParam in RANGE_MS) {
    timeRange = rangeParam as TimeRange;
  }
  const viewParam = sp.get("view");
  if (viewParam === "flat" || viewParam === "threaded") {
    viewMode = viewParam;
  }
}

/** Sync store state → URL query params (replaceState). */
export function syncToURL(): void {
  const sp = new URLSearchParams(window.location.search);
  if (filterRepo) sp.set("repo", filterRepo);
  else sp.delete("repo");
  if (filterTypes.length > 0) sp.set("types", filterTypes.join(","));
  else sp.delete("types");
  if (searchQuery) sp.set("search", searchQuery);
  else sp.delete("search");
  if (timeRange !== "7d") sp.set("range", timeRange);
  else sp.delete("range");
  if (viewMode !== "flat") sp.set("view", viewMode);
  else sp.delete("view");
  const qs = sp.toString();
  const url = "/" + (qs ? `?${qs}` : "");
  history.replaceState(null, "", url);
}
