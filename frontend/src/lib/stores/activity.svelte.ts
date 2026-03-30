import { listActivity } from "../api/activity.js";
import type { ActivityItem, ActivityParams } from "../api/activity.js";

// --- state ---

let items = $state<ActivityItem[]>([]);
let loading = $state(false);
let error = $state<string | null>(null);
let hasMore = $state(false);
let filterRepo = $state<string | undefined>(undefined);
let filterTypes = $state<string[]>([]);
let searchQuery = $state<string | undefined>(undefined);
let pollHandle: ReturnType<typeof setInterval> | null = null;
let activeController: AbortController | null = null;
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

export function hasMoreActivity(): boolean {
  return hasMore;
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

function buildParams(): ActivityParams {
  const p: ActivityParams = { limit: 50 };
  if (filterRepo) p.repo = filterRepo;
  if (filterTypes.length > 0) p.types = filterTypes;
  if (searchQuery) p.search = searchQuery;
  return p;
}

function cancelPending(): void {
  if (activeController) {
    activeController.abort();
    activeController = null;
  }
}

/** Load the feed from the top (initial load or after filter change). */
export async function loadActivity(): Promise<void> {
  cancelPending();
  const version = ++requestVersion;
  loading = true;
  error = null;
  try {
    const resp = await listActivity(buildParams());
    if (version !== requestVersion) return;
    items = resp.items;
    hasMore = resp.has_more;
  } catch (err) {
    if (version !== requestVersion) return;
    error = err instanceof Error ? err.message : String(err);
  } finally {
    if (version === requestVersion) loading = false;
  }
}

/** Load more items (append to existing list). */
export async function loadMoreActivity(): Promise<void> {
  if (items.length === 0) return;
  const lastItem = items[items.length - 1]!;
  const version = ++requestVersion;
  loading = true;
  error = null;
  try {
    const params = buildParams();
    params.before = lastItem.cursor;
    const resp = await listActivity(params);
    if (version !== requestVersion) return;
    items = [...items, ...resp.items];
    hasMore = resp.has_more;
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
    if (resp.has_more) {
      // More new items than one page — full reload.
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

/** Sync URL query params → store state. Called when navigating to /. */
export function syncFromURL(): void {
  const sp = new URLSearchParams(window.location.search);
  filterRepo = sp.get("repo") ?? undefined;
  const typesParam = sp.get("types");
  filterTypes = typesParam ? typesParam.split(",") : [];
  searchQuery = sp.get("search") ?? undefined;
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
  const qs = sp.toString();
  const url = "/" + (qs ? `?${qs}` : "");
  history.replaceState(null, "", url);
}
