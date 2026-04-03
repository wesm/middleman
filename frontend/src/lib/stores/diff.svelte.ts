import type { DiffResult } from "../api/types.js";

let diff = $state<DiffResult | null>(null);
let loading = $state(false);
let error = $state<string | null>(null);
let abortController: AbortController | null = null;

// Toolbar preferences (persisted in localStorage).
const VALID_TAB_WIDTHS = [1, 2, 4, 8];
function loadTabWidth(): number {
  const raw = parseInt(localStorage.getItem("diff-tab-width") ?? "4", 10);
  return VALID_TAB_WIDTHS.includes(raw) ? raw : 4;
}
let tabWidth = $state(loadTabWidth());
let hideWhitespace = $state(localStorage.getItem("diff-hide-whitespace") === "true");

// Per-file collapse state, keyed by "{owner}/{name}#{number}".
// Persisted in localStorage. Uses plain arrays (not Sets) for Svelte reactivity.
function loadCollapsedFiles(): Record<string, string[]> {
  try {
    const raw = localStorage.getItem("diff-collapsed-files");
    if (!raw) return {};
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) return {};
    const result: Record<string, string[]> = {};
    for (const [key, value] of Object.entries(parsed as Record<string, unknown>)) {
      if (Array.isArray(value) && value.every((v) => typeof v === "string")) {
        result[key] = value as string[];
      }
    }
    return result;
  } catch {
    return {};
  }
}

function saveCollapsedFiles(cf: Record<string, string[]>): void {
  localStorage.setItem("diff-collapsed-files", JSON.stringify(cf));
}

let collapsedFiles = $state<Record<string, string[]>>(loadCollapsedFiles());

// Active file in the diff (shared between sidebar file tree and diff area).
let activeFile = $state<string | null>(null);
// When set, DiffView should scroll to this file path and then clear it.
let scrollTarget = $state<string | null>(null);
// True during programmatic smooth scrolls — suppresses scroll-based activeFile updates.
let scrolling = $state(false);

export function getDiff(): DiffResult | null { return diff; }
export function isDiffLoading(): boolean { return loading; }
export function getDiffError(): string | null { return error; }
export function getTabWidth(): number { return tabWidth; }
export function getHideWhitespace(): boolean { return hideWhitespace; }
export function getActiveFile(): string | null { return activeFile; }
export function setActiveFile(path: string | null): void { activeFile = path; }
export function isScrolling(): boolean { return scrolling; }
export function setScrolling(v: boolean): void { scrolling = v; }
export function requestScrollToFile(path: string): void {
  activeFile = path;
  scrollTarget = path;
  scrolling = true;
}
export function consumeScrollTarget(): string | null {
  const target = scrollTarget;
  scrollTarget = null;
  return target;
}

export function setTabWidth(w: number): void {
  tabWidth = w;
  localStorage.setItem("diff-tab-width", String(w));
}

// Track current diff context for re-fetching on toggle change.
let currentOwner = "";
let currentName = "";
let currentNumber = 0;

export function setHideWhitespace(v: boolean): void {
  hideWhitespace = v;
  localStorage.setItem("diff-hide-whitespace", String(v));
  // Re-fetch the diff with the new whitespace setting.
  if (currentOwner && currentName && currentNumber) {
    void loadDiff(currentOwner, currentName, currentNumber);
  }
}

export function isFileCollapsed(owner: string, name: string, number: number, filePath: string): boolean {
  const key = `${owner}/${name}#${number}`;
  return (collapsedFiles[key] ?? []).includes(filePath);
}

export function toggleFileCollapsed(owner: string, name: string, number: number, filePath: string): void {
  const key = `${owner}/${name}#${number}`;
  const current = collapsedFiles[key] ?? [];
  // Replace the array (not mutate) so Svelte detects the change.
  if (current.includes(filePath)) {
    collapsedFiles = { ...collapsedFiles, [key]: current.filter((f) => f !== filePath) };
  } else {
    collapsedFiles = { ...collapsedFiles, [key]: [...current, filePath] };
  }
  saveCollapsedFiles(collapsedFiles);
}

export async function loadDiff(owner: string, name: string, number: number): Promise<void> {
  currentOwner = owner;
  currentName = name;
  currentNumber = number;

  // Abort any in-flight request before starting a new one.
  abortController?.abort();
  const ac = new AbortController();
  abortController = ac;

  loading = true;
  error = null;
  try {
    const basePath = window.__BASE_PATH__ ?? "/";
    const url = `${basePath}api/v1/repos/${encodeURIComponent(owner)}/${encodeURIComponent(name)}/pulls/${number}/diff${hideWhitespace ? "?whitespace=hide" : ""}`;
    const response = await fetch(url, { signal: ac.signal });
    if (!response.ok) {
      const body = await response.json().catch(() => ({}));
      throw new Error(
        (body as Record<string, string>).detail
          ?? (body as Record<string, string>).title
          ?? `HTTP ${response.status}`,
      );
    }
    const data = await response.json();
    // Guard: a newer request may have replaced abortController while we awaited.
    if (abortController !== ac) return;
    diff = data;
  } catch (err) {
    if (ac.signal.aborted) return;
    if (abortController !== ac) return;
    error = err instanceof Error ? err.message : String(err);
    diff = null;
  } finally {
    if (!ac.signal.aborted && abortController === ac) {
      loading = false;
    }
  }
}

export function clearDiff(): void {
  abortController?.abort();
  abortController = null;
  diff = null;
  error = null;
  loading = false;
  activeFile = null;
  scrollTarget = null;
  currentOwner = "";
  currentName = "";
  currentNumber = 0;
}
