import type { DiffResult } from "../api/types.js";

export interface DiffStoreOptions {
  getBasePath?: () => string;
}

function safeGetItem(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function safeSetItem(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* ignore */
  }
}

const VALID_TAB_WIDTHS = [1, 2, 4, 8];

function loadTabWidth(): number {
  const raw = parseInt(
    safeGetItem("diff-tab-width") ?? "4",
    10,
  );
  return VALID_TAB_WIDTHS.includes(raw) ? raw : 4;
}

function loadCollapsedFiles(): Record<string, string[]> {
  try {
    const raw = safeGetItem("diff-collapsed-files");
    if (!raw) return {};
    const parsed: unknown = JSON.parse(raw);
    if (
      typeof parsed !== "object" ||
      parsed === null ||
      Array.isArray(parsed)
    )
      return {};
    const result: Record<string, string[]> = {};
    for (const [key, value] of Object.entries(
      parsed as Record<string, unknown>,
    )) {
      if (
        Array.isArray(value) &&
        value.every((v) => typeof v === "string")
      ) {
        result[key] = value as string[];
      }
    }
    return result;
  } catch {
    return {};
  }
}

function saveCollapsedFiles(
  cf: Record<string, string[]>,
): void {
  safeSetItem("diff-collapsed-files", JSON.stringify(cf));
}

export function createDiffStore(opts?: DiffStoreOptions) {
  const getBasePath = opts?.getBasePath ?? (() => "/");

  let diff = $state<DiffResult | null>(null);
  let loading = $state(false);
  let storeError = $state<string | null>(null);
  let abortController: AbortController | null = null;

  let tabWidth = $state(loadTabWidth());
  let hideWhitespace = $state(
    safeGetItem("diff-hide-whitespace") === "true",
  );
  let collapsedFiles = $state<Record<string, string[]>>(
    loadCollapsedFiles(),
  );
  let activeFile = $state<string | null>(null);
  let scrollTarget = $state<string | null>(null);
  let scrolling = $state(false);

  let currentOwner = "";
  let currentName = "";
  let currentNumber = 0;

  // --- reads ---

  function getDiff(): DiffResult | null {
    return diff;
  }
  function isDiffLoading(): boolean {
    return loading;
  }
  function getDiffError(): string | null {
    return storeError;
  }
  function getTabWidth(): number {
    return tabWidth;
  }
  function getHideWhitespace(): boolean {
    return hideWhitespace;
  }
  function getActiveFile(): string | null {
    return activeFile;
  }
  function isScrolling(): boolean {
    return scrolling;
  }

  function isFileCollapsed(
    owner: string,
    name: string,
    number: number,
    filePath: string,
  ): boolean {
    const key = `${owner}/${name}#${number}`;
    return (collapsedFiles[key] ?? []).includes(filePath);
  }

  // --- writes ---

  function setActiveFile(path: string | null): void {
    activeFile = path;
  }

  function clearScrolling(): void {
    scrolling = false;
  }

  function requestScrollToFile(path: string): void {
    activeFile = path;
    scrollTarget = path;
    scrolling = true;
  }

  function consumeScrollTarget(): string | null {
    const target = scrollTarget;
    scrollTarget = null;
    return target;
  }

  function setTabWidth(w: number): void {
    tabWidth = w;
    safeSetItem("diff-tab-width", String(w));
  }

  function setHideWhitespace(v: boolean): void {
    hideWhitespace = v;
    safeSetItem("diff-hide-whitespace", String(v));
    if (currentOwner && currentName && currentNumber) {
      void loadDiff(
        currentOwner,
        currentName,
        currentNumber,
      );
    }
  }

  function toggleFileCollapsed(
    owner: string,
    name: string,
    number: number,
    filePath: string,
  ): void {
    const key = `${owner}/${name}#${number}`;
    const current = collapsedFiles[key] ?? [];
    if (current.includes(filePath)) {
      collapsedFiles = {
        ...collapsedFiles,
        [key]: current.filter((f) => f !== filePath),
      };
    } else {
      collapsedFiles = {
        ...collapsedFiles,
        [key]: [...current, filePath],
      };
    }
    saveCollapsedFiles(collapsedFiles);
  }

  async function loadDiff(
    owner: string,
    name: string,
    number: number,
  ): Promise<void> {
    currentOwner = owner;
    currentName = name;
    currentNumber = number;

    abortController?.abort();
    const ac = new AbortController();
    abortController = ac;

    loading = true;
    storeError = null;
    try {
      const basePath = getBasePath();
      const url =
        `${basePath}api/v1/repos/` +
        `${encodeURIComponent(owner)}/` +
        `${encodeURIComponent(name)}/` +
        `pulls/${number}/diff` +
        `${hideWhitespace ? "?whitespace=hide" : ""}`;
      const response = await fetch(url, {
        signal: ac.signal,
      });
      if (!response.ok) {
        const body = await response
          .json()
          .catch(() => ({}));
        throw new Error(
          (body as Record<string, string>).detail ??
            (body as Record<string, string>).title ??
            `HTTP ${response.status}`,
        );
      }
      const data = await response.json();
      if (abortController !== ac) return;
      diff = data as DiffResult;
      const files = (data as DiffResult).files;
      if (
        !files?.some(
          (f: { path: string }) => f.path === activeFile,
        )
      ) {
        activeFile = files?.[0]?.path ?? null;
      }
    } catch (err) {
      if (ac.signal.aborted) return;
      if (abortController !== ac) return;
      storeError =
        err instanceof Error ? err.message : String(err);
      diff = null;
    } finally {
      if (!ac.signal.aborted && abortController === ac) {
        loading = false;
      }
    }
  }

  function clearDiff(): void {
    abortController?.abort();
    abortController = null;
    diff = null;
    storeError = null;
    loading = false;
    activeFile = null;
    scrollTarget = null;
    scrolling = false;
    currentOwner = "";
    currentName = "";
    currentNumber = 0;
  }

  return {
    getDiff,
    isDiffLoading,
    getDiffError,
    getTabWidth,
    getHideWhitespace,
    getActiveFile,
    setActiveFile,
    isScrolling,
    clearScrolling,
    requestScrollToFile,
    consumeScrollTarget,
    setTabWidth,
    setHideWhitespace,
    isFileCollapsed,
    toggleFileCollapsed,
    loadDiff,
    clearDiff,
  };
}

export type DiffStore = ReturnType<typeof createDiffStore>;
