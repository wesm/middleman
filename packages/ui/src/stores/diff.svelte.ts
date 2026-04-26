import type {
  DiffResult,
  FilesResult,
  CommitInfo,
} from "../api/types.js";
import { createAPIClient } from "../api/generated/client.js";
import type { components } from "../api/generated/schema.js";
import type { MiddlemanClient } from "../types.js";

export type DiffScope =
  | { kind: "head" }
  | { kind: "commit"; sha: string }
  | { kind: "range"; fromSha: string; toSha: string };

export interface DiffStoreOptions {
  client?: MiddlemanClient;
  getBasePath?: () => string;
}

function apiErrorMessage(
  error: { detail?: string; title?: string } | undefined,
  fallback: string,
): string {
  return error?.detail ?? error?.title ?? fallback;
}

type DiffResponse = components["schemas"]["DiffResponse"];
type FilesResponse = components["schemas"]["FilesResponse"];

function normalizeDiffResult(data: DiffResponse): DiffResult {
  return {
    ...data,
    files: data.files ?? [],
  } as DiffResult;
}

function normalizeFilesResult(data: FilesResponse): FilesResult {
  return {
    ...data,
    files: data.files ?? [],
  } as FilesResult;
}

function apiBaseURL(basePath: string): string {
  const path = `${basePath.replace(/\/$/, "")}/api/v1`;
  if (typeof window !== "undefined") {
    return new URL(path, window.location.origin).toString();
  }
  return `http://localhost${path}`;
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
  const apiClient =
    opts?.client ??
    createAPIClient(apiBaseURL(getBasePath()), {
      fetch: globalThis.fetch.bind(globalThis),
    });

  let diff = $state<DiffResult | null>(null);
  let loading = $state(false);
  let storeError = $state<string | null>(null);
  let abortController: AbortController | null = null;

  let fileList = $state<FilesResult | null>(null);
  let fileListLoading = $state(false);
  let fileListAbortController: AbortController | null = null;

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
  let commits = $state<CommitInfo[] | null>(null);
  let commitsLoading = $state(false);
  let commitsError = $state<string | null>(null);
  let scope = $state<DiffScope>({ kind: "head" });

  let currentOwner = $state("");
  let currentName = $state("");
  let currentNumber = $state(0);

  function getCurrentPR(): { owner: string; name: string; number: number } | null {
    if (!currentOwner) return null;
    return { owner: currentOwner, name: currentName, number: currentNumber };
  }

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
  function getFileList(): FilesResult | null {
    // Prefer diff.files once available — it respects hideWhitespace
    // and is authoritative. The lightweight /files response is a fast
    // preview used only until the full diff arrives.
    if (diff) return { stale: diff.stale, files: diff.files ?? [] };
    if (fileList) return { stale: fileList.stale, files: fileList.files ?? [] };
    return null;
  }
  function isFileListLoading(): boolean {
    // Show loading until we have *some* file data. When /files fails
    // but /diff is still in flight, keep showing loading state.
    return !diff && (fileListLoading || loading);
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

  function getScrollTarget(): string | null {
    return scrollTarget;
  }

  function consumeScrollTarget(): void {
    scrollTarget = null;
  }

  function setTabWidth(w: number): void {
    tabWidth = w;
    safeSetItem("diff-tab-width", String(w));
  }

  function setHideWhitespace(v: boolean): void {
    hideWhitespace = v;
    safeSetItem("diff-hide-whitespace", String(v));
    if (currentOwner && currentName && currentNumber) {
      void reloadDiffOnly();
    }
  }

  async function reloadDiffOnly(): Promise<void> {
    abortController?.abort();
    // Abort any in-flight /files request so a late response from a
    // prior loadDiff() cannot repopulate fileList after we clear it.
    fileListAbortController?.abort();
    fileListAbortController = null;
    fileListLoading = false;
    const ac = new AbortController();
    abortController = ac;
    fileList = null;

    loading = true;
    storeError = null;
    try {
      const { data, error, response } = await apiClient.GET(
        "/repos/{owner}/{name}/pulls/{number}/diff",
        {
          params: {
            path: {
              owner: currentOwner,
              name: currentName,
              number: currentNumber,
            },
            query: diffQuery(),
          },
          signal: ac.signal,
        },
      );
      if (abortController !== ac) return;
      if (!data) {
        throw new Error(apiErrorMessage(error, `HTTP ${response.status}`));
      }
      const result = normalizeDiffResult(data);
      diff = result;
      setActiveIfNeeded(result.files);
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

  function diffQuery(): {
    whitespace?: string;
    commit?: string;
    from?: string;
    to?: string;
  } {
    return {
      ...(hideWhitespace && { whitespace: "hide" }),
      ...(scope.kind === "commit" && { commit: scope.sha }),
      ...(scope.kind === "range" && {
        from: scope.fromSha,
        to: scope.toSha,
      }),
    };
  }

  function setActiveIfNeeded(
    files: { path: string }[] | undefined,
  ): void {
    if (
      !files?.some((f) => f.path === activeFile)
    ) {
      activeFile = files?.[0]?.path ?? null;
    }
  }

  async function loadDiff(
    owner: string,
    name: string,
    number: number,
  ): Promise<void> {
    const prChanged =
      owner !== currentOwner ||
      name !== currentName ||
      number !== currentNumber;
    currentOwner = owner;
    currentName = name;
    currentNumber = number;
    if (prChanged) {
      scope = { kind: "head" };
      commits = null;
      commitsLoading = false;
      commitsError = null;
    }

    abortController?.abort();
    fileListAbortController?.abort();
    const diffAc = new AbortController();
    const filesAc = new AbortController();
    abortController = diffAc;
    fileListAbortController = filesAc;

    diff = null;
    fileList = null;
    loading = true;
    fileListLoading = true;
    storeError = null;

    const filesPromise = (async () => {
      try {
        const { data } = await apiClient.GET(
          "/repos/{owner}/{name}/pulls/{number}/files",
          {
            params: { path: { owner, name, number } },
            signal: filesAc.signal,
          },
        );
        if (fileListAbortController !== filesAc) return;
        if (!data) return;
        const result = normalizeFilesResult(data);
        fileList = result;
        setActiveIfNeeded(result.files);
      } catch {
        if (filesAc.signal.aborted) return;
        if (fileListAbortController !== filesAc) return;
        fileList = null;
      } finally {
        if (
          !filesAc.signal.aborted &&
          fileListAbortController === filesAc
        ) {
          fileListLoading = false;
        }
      }
    })();

    const diffPromise = (async () => {
      try {
        const { data, error, response } = await apiClient.GET(
          "/repos/{owner}/{name}/pulls/{number}/diff",
          {
            params: {
              path: { owner, name, number },
              query: diffQuery(),
            },
            signal: diffAc.signal,
          },
        );
        if (abortController !== diffAc) return;
        if (!data) {
          throw new Error(apiErrorMessage(error, `HTTP ${response.status}`));
        }
        const result = normalizeDiffResult(data);
        diff = result;
        setActiveIfNeeded(result.files);
      } catch (_err) {
        if (diffAc.signal.aborted) return;
        if (abortController !== diffAc) return;
        storeError =
          _err instanceof Error ? _err.message : String(_err);
        diff = null;
        fileList = null;
        // Invalidate and abort /files so a late response cannot repopulate.
        fileListAbortController = null;
        filesAc.abort();
        fileListLoading = false;
      } finally {
        if (
          !diffAc.signal.aborted &&
          abortController === diffAc
        ) {
          loading = false;
        }
      }
    })();

    await Promise.allSettled([filesPromise, diffPromise]);
  }

  function clearDiff(): void {
    abortController?.abort();
    abortController = null;
    fileListAbortController?.abort();
    fileListAbortController = null;
    diff = null;
    fileList = null;
    storeError = null;
    loading = false;
    fileListLoading = false;
    activeFile = null;
    scrollTarget = null;
    scrolling = false;
    commits = null;
    commitsLoading = false;
    commitsError = null;
    scope = { kind: "head" };
    currentOwner = "";
    currentName = "";
    currentNumber = 0;
  }

  async function loadCommits(): Promise<void> {
    if (commits || commitsLoading) return;
    if (!currentOwner || !currentName || !currentNumber) return;

    commitsLoading = true;
    commitsError = null;
    const owner = currentOwner;
    const name = currentName;
    const number = currentNumber;
    try {
      const { data, error, response } = await apiClient.GET(
        "/repos/{owner}/{name}/pulls/{number}/commits",
        {
          params: { path: { owner, name, number } },
        },
      );
      if (currentOwner !== owner || currentName !== name || currentNumber !== number) return;
      if (!data) {
        throw new Error(apiErrorMessage(error, `HTTP ${response.status}`));
      }
      if (currentOwner !== owner || currentName !== name || currentNumber !== number) return;
      commits = data.commits ?? [];
    } catch (err) {
      if (currentOwner !== owner || currentName !== name || currentNumber !== number) return;
      commitsError = err instanceof Error ? err.message : String(err);
    } finally {
      if (currentOwner === owner && currentName === name && currentNumber === number) {
        commitsLoading = false;
      }
    }
  }

  function getScope(): DiffScope {
    return scope;
  }

  function getCommits(): CommitInfo[] | null {
    return commits;
  }

  function isCommitsLoading(): boolean {
    return commitsLoading;
  }

  function getCommitsError(): string | null {
    return commitsError;
  }

  function selectCommit(sha: string): void {
    scope = { kind: "commit", sha };
    if (currentOwner && currentName && currentNumber) {
      void loadDiff(currentOwner, currentName, currentNumber);
    }
  }

  function selectRange(fromSha: string, toSha: string): void {
    if (!commits) return;
    const fromIdx = commits.findIndex((c) => c.sha === fromSha);
    const toIdx = commits.findIndex((c) => c.sha === toSha);
    if (fromIdx === -1 || toIdx === -1) return;
    const [older, newer] = fromIdx > toIdx ? [fromSha, toSha] : [toSha, fromSha];
    scope = { kind: "range", fromSha: older, toSha: newer };
    if (currentOwner && currentName && currentNumber) {
      void loadDiff(currentOwner, currentName, currentNumber);
    }
  }

  function resetToHead(): void {
    scope = { kind: "head" };
    if (currentOwner && currentName && currentNumber) {
      void loadDiff(currentOwner, currentName, currentNumber);
    }
  }

  function stepPrev(): void {
    if (!commits) {
      void loadCommits();
      return;
    }
    if (commits.length === 0) return;
    const s = scope;
    if (s.kind === "head") {
      selectCommit(commits[0]!.sha);
    } else if (s.kind === "commit") {
      const idx = commits.findIndex((c) => c.sha === s.sha);
      if (idx < commits.length - 1) selectCommit(commits[idx + 1]!.sha);
    } else {
      selectCommit(s.fromSha);
    }
  }

  function stepNext(): void {
    if (!commits) {
      void loadCommits();
      return;
    }
    if (commits.length === 0) return;
    const s = scope;
    if (s.kind === "head") {
      return;
    } else if (s.kind === "commit") {
      const idx = commits.findIndex((c) => c.sha === s.sha);
      if (idx > 0) {
        selectCommit(commits[idx - 1]!.sha);
      } else {
        resetToHead();
      }
    } else {
      selectCommit(s.toSha);
    }
  }

  return {
    getDiff,
    getCurrentPR,
    isDiffLoading,
    getDiffError,
    getFileList,
    isFileListLoading,
    getTabWidth,
    getHideWhitespace,
    getActiveFile,
    setActiveFile,
    isScrolling,
    clearScrolling,
    requestScrollToFile,
    getScrollTarget,
    consumeScrollTarget,
    setTabWidth,
    setHideWhitespace,
    isFileCollapsed,
    toggleFileCollapsed,
    loadDiff,
    clearDiff,
    getScope,
    getCommits,
    isCommitsLoading,
    getCommitsError,
    loadCommits,
    selectCommit,
    selectRange,
    resetToHead,
    stepPrev,
    stepNext,
  };
}

export type DiffStore = ReturnType<typeof createDiffStore>;
