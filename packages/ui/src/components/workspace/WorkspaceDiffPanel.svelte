<script lang="ts">
  import DiffSidebar from "../diff/DiffSidebar.svelte";
  import DiffScopePicker from "../diff/DiffScopePicker.svelte";
  import DiffToolbar from "../diff/DiffToolbar.svelte";
  import SplitResizeHandle from "../shared/SplitResizeHandle.svelte";
  import type { SplitResizeEvent } from "../shared/split-resize.js";
  import DiffView from "../diff/DiffView.svelte";
  import { getStores } from "../../context.js";
  import type { WorkspaceDiffBase } from "../../stores/diff.svelte.js";

  interface Props {
    workspaceID: string;
    provider: string;
    platformHost?: string | undefined;
    repoOwner: string;
    repoName: string;
    repoPath: string;
    itemNumber: number;
    active?: boolean;
  }

  const {
    workspaceID,
    provider,
    platformHost,
    repoOwner,
    repoName,
    repoPath,
    itemNumber,
    active = false,
  }: Props = $props();
  const { diff } = getStores();

  const storageKey = "workspace-diff-file-list-width";
  const hiddenStorageKey = "workspace-diff-file-list-hidden";
  const defaultFileListWidth = 280;
  const minFileListWidth = 200;
  const maxFileListWidth = 520;
  const minDiffPaneWidth = 320;
  const resizeHandleWidth = 4;
  const compactLayoutWidth = 720;
  let fileListResizeStartWidth = 0;

  let base = $state<WorkspaceDiffBase>("head");
  let loadedKey = "";
  let workspaceDiffLayout = $state<HTMLDivElement>();
  let workspaceDiffLayoutWidth = $state(0);
  let fileListWidth = $state(loadFileListWidth());
  let fileListHidden = $state(loadFileListHidden());
  const resetKey = $derived(`${workspaceID}:${base}`);

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

  function layoutMaxFileListWidth(): number {
    if (workspaceDiffLayoutWidth <= 0) return maxFileListWidth;
    if (workspaceDiffLayoutWidth <= compactLayoutWidth) return maxFileListWidth;
    return Math.max(
      0,
      workspaceDiffLayoutWidth - minDiffPaneWidth - resizeHandleWidth,
    );
  }

  function minAllowedFileListWidth(): number {
    return Math.min(minFileListWidth, layoutMaxFileListWidth());
  }

  function maxAllowedFileListWidth(): number {
    return Math.min(maxFileListWidth, layoutMaxFileListWidth());
  }

  function clampFileListWidth(width: number): number {
    return Math.max(
      minAllowedFileListWidth(),
      Math.min(maxAllowedFileListWidth(), Math.round(width)),
    );
  }

  function loadFileListWidth(): number {
    const raw = Number.parseInt(safeGetItem(storageKey) ?? "", 10);
    if (!Number.isFinite(raw)) return defaultFileListWidth;
    return clampFileListWidth(raw);
  }

  function loadFileListHidden(): boolean {
    return safeGetItem(hiddenStorageKey) === "true";
  }

  function updateWorkspaceDiffLayoutWidth(width: number): void {
    if (!Number.isFinite(width) || width <= 0) return;
    workspaceDiffLayoutWidth = Math.round(width);
    if (workspaceDiffLayoutWidth > compactLayoutWidth) {
      fileListWidth = clampFileListWidth(fileListWidth);
    }
  }

  $effect(() => {
    if (!active) return;
    const key = `${workspaceID}:${base}:${fileListHidden ? "stacked" : "single"}`;
    if (loadedKey === key) return;
    loadedKey = key;
    void diff.loadWorkspaceDiff(workspaceID, base, fileListHidden);
  });

  $effect(() => {
    const layout = workspaceDiffLayout;
    if (!layout) return;

    updateWorkspaceDiffLayoutWidth(layout.getBoundingClientRect().width);
    if (typeof ResizeObserver === "undefined") return;

    const observer = new ResizeObserver((entries) => {
      updateWorkspaceDiffLayoutWidth(
        entries[0]?.contentRect.width ?? layout.getBoundingClientRect().width,
      );
    });
    observer.observe(layout);

    return () => {
      observer.disconnect();
    };
  });

  function selectBase(nextBase: WorkspaceDiffBase): void {
    base = nextBase;
  }

  function toggleFileList(): void {
    fileListHidden = !fileListHidden;
    safeSetItem(hiddenStorageKey, String(fileListHidden));
  }

  function handleFileListResizeStart(): void {
    fileListResizeStartWidth = fileListWidth;
  }

  function widthFromResize(event: SplitResizeEvent): number {
    return clampFileListWidth(fileListResizeStartWidth + event.deltaX);
  }

  function handleFileListResize(event: SplitResizeEvent): void {
    fileListWidth = widthFromResize(event);
  }

  function handleFileListResizeEnd(event: SplitResizeEvent): void {
    const finalWidth = widthFromResize(event);
    fileListWidth = finalWidth;
    safeSetItem(storageKey, String(finalWidth));
  }
</script>

<section class="workspace-diff" aria-label="Workspace Diff">
  <div class="workspace-diff-scope">
    <span class="scope-label">Compare</span>
    <div class="scope-toggle" role="group" aria-label="Workspace diff base">
      <button
        class="scope-btn"
        class:scope-btn--active={base === "head"}
        aria-pressed={base === "head"}
        aria-label="Compare with HEAD"
        title="HEAD"
        onclick={() => selectBase("head")}
      >
        HEAD
      </button>
      <button
        class="scope-btn scope-btn--wide"
        class:scope-btn--active={base === "pushed"}
        aria-pressed={base === "pushed"}
        aria-label="Compare with pushed branch"
        title="Pushed branch"
        onclick={() => selectBase("pushed")}
      >
        Branch
      </button>
      <button
        class="scope-btn scope-btn--wide"
        class:scope-btn--active={base === "merge-target"}
        aria-pressed={base === "merge-target"}
        aria-label="Compare with merge target"
        title="Merge target"
        onclick={() => selectBase("merge-target")}
      >
        Target
      </button>
    </div>
    <DiffScopePicker compact />
  </div>
  <DiffToolbar
    compact
    {fileListHidden}
    onToggleFileList={toggleFileList}
    showRichPreview={false}
    showScopePicker={false}
  />
  <div class="workspace-diff-layout" bind:this={workspaceDiffLayout}>
    {#if !fileListHidden}
      <aside
        class="workspace-diff-sidebar"
        aria-label="Changed files"
        style:--workspace-diff-file-list-width={`${fileListWidth}px`}
      >
        <DiffSidebar showCommits={false} {resetKey} />
      </aside>
      <SplitResizeHandle
        class="workspace-diff-resize-handle"
        ariaLabel="Resize workspace file list"
        onResizeStart={handleFileListResizeStart}
        onResize={handleFileListResize}
        onResizeEnd={handleFileListResizeEnd}
      />
    {/if}
    <div class="workspace-diff-main">
      <DiffView
        {provider}
        {platformHost}
        owner={repoOwner}
        name={repoName}
        {repoPath}
        number={itemNumber}
        loadOnMount={false}
        keyboardActive={false}
        richPreviewEnabled={false}
      />
    </div>
  </div>
</section>

<style>
  .workspace-diff {
    display: flex;
    flex-direction: column;
    container-type: inline-size;
    height: 100%;
    min-width: 0;
    overflow: hidden;
    background: var(--diff-bg);
  }

  .workspace-diff-scope {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 32px;
    padding: 5px 12px;
    border-bottom: 1px solid var(--diff-border);
    background: var(--bg-surface);
    flex-shrink: 0;
  }

  .workspace-diff-scope :global(.diff-scope-picker) {
    margin-left: auto;
  }

  .scope-label {
    color: var(--text-secondary);
    font-size: var(--font-size-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }

  .scope-toggle {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    width: 184px;
    flex: 0 1 184px;
    min-width: 164px;
    padding: 2px;
    border: 1px solid var(--border-muted);
    border-radius: 4px;
    background: var(--bg-inset);
  }

  .scope-btn {
    min-width: 0;
    height: 22px;
    padding: 0 6px;
    border: 0;
    border-radius: 3px;
    background: transparent;
    color: var(--text-muted);
    font-size: var(--font-size-xs);
    font-weight: 600;
    cursor: pointer;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .scope-btn:hover {
    color: var(--text-primary);
  }

  .scope-btn--active {
    background: var(--accent-blue);
    color: #fff;
  }

  .scope-btn--active:hover {
    color: #fff;
  }

  .workspace-diff-layout {
    display: flex;
    flex: 1;
    min-height: 0;
    overflow: hidden;
  }

  .workspace-diff-sidebar {
    display: flex;
    flex-direction: column;
    width: var(--workspace-diff-file-list-width, 280px);
    flex-shrink: 0;
    overflow-y: auto;
    border-right: 1px solid var(--border-default);
    background: var(--bg-surface);
  }

  .workspace-diff-main {
    display: flex;
    flex: 1;
    min-width: 0;
    overflow: hidden;
  }

  @container (max-width: 720px) {
    .workspace-diff-layout {
      flex-direction: column;
    }

    .workspace-diff-sidebar {
      width: 100%;
      max-height: 35vh;
      border-right: none;
      border-bottom: 1px solid var(--border-default);
    }

    :global(.workspace-diff-resize-handle) {
      display: none;
    }
  }
</style>
