import type { DiffResult, FilesResult, CommitInfo } from "../api/types.js";

export type DiffScope =
  | { kind: "head" }
  | { kind: "commit"; sha: string }
  | { kind: "range"; fromSha: string; toSha: string };

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
      const basePath = getBasePath();
      const reloadParams = new URLSearchParams();
      if (hideWhitespace) reloadParams.set("whitespace", "hide");
      if (scope.kind === "commit") reloadParams.set("commit", scope.sha);
      if (scope.kind === "range") {
        reloadParams.set("from", scope.fromSha);
        reloadParams.set("to", scope.toSha);
      }
      const reloadQs = reloadParams.toString();
      const url =
        `${basePath}api/v1/repos/` +
        `${encodeURIComponent(currentOwner)}/` +
        `${encodeURIComponent(currentName)}/` +
        `pulls/${currentNumber}/diff` +
        (reloadQs ? `?${reloadQs}` : "");
      const data = await fetchJSON(url, ac.signal);
      if (abortController !== ac) return;
      diff = data as DiffResult;
      setActiveIfNeeded((data as DiffResult).files);
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

  function fetchJSON(
    url: string,
    signal: AbortSignal,
  ): Promise<unknown> {
    return fetch(url, { signal }).then(async (r) => {
      if (!r.ok) {
        const body = await r.json().catch(() => ({}));
        throw new Error(
          (body as Record<string, string>).detail ??
            (body as Record<string, string>).title ??
            `HTTP ${r.status}`,
        );
      }
      return r.json();
    });
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

    const basePath = getBasePath();
    const prefix =
      `${basePath}api/v1/repos/` +
      `${encodeURIComponent(owner)}/` +
      `${encodeURIComponent(name)}/` +
      `pulls/${number}`;

    const filesPromise = (async () => {
      try {
        const data = await fetchJSON(
          `${prefix}/files`,
          filesAc.signal,
        );
        if (fileListAbortController !== filesAc) return;
        fileList = data as FilesResult;
        setActiveIfNeeded((data as FilesResult).files);
      } catch (err) {
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
        const params = new URLSearchParams();
        if (hideWhitespace) params.set("whitespace", "hide");
        if (scope.kind === "commit") params.set("commit", scope.sha);
        if (scope.kind === "range") {
          params.set("from", scope.fromSha);
          params.set("to", scope.toSha);
        }
        const qs = params.toString();
        const url = `${prefix}/diff${qs ? `?${qs}` : ""}`;
        const data = await fetchJSON(url, diffAc.signal);
        if (abortController !== diffAc) return;
        diff = data as DiffResult;
        setActiveIfNeeded((data as DiffResult).files);
      } catch (err) {
        if (diffAc.signal.aborted) return;
        if (abortController !== diffAc) return;
        storeError =
          err instanceof Error ? err.message : String(err);
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
      const basePath = getBasePath();
      const url =
        `${basePath}api/v1/repos/` +
        `${encodeURIComponent(owner)}/` +
        `${encodeURIComponent(name)}/` +
        `pulls/${number}/commits`;
      const response = await fetch(url);
      if (currentOwner !== owner || currentName !== name || currentNumber !== number) return;
      if (!response.ok) {
        const body = await response.json().catch(() => ({}));
        throw new Error(
          (body as Record<string, string>).detail ??
            (body as Record<string, string>).title ??
            `HTTP ${response.status}`,
        );
      }
      const data = (await response.json()) as { commits: CommitInfo[] };
      if (currentOwner !== owner || currentName !== name || currentNumber !== number) return;
      commits = data.commits;
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
