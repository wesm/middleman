<script lang="ts">
  import { onDestroy } from "svelte";
  import SplitResizeHandle from "../shared/SplitResizeHandle.svelte";
  import type { SplitResizeEvent } from "../shared/split-resize.js";
  import DiffSidebar from "./DiffSidebar.svelte";
  import DiffToolbar from "./DiffToolbar.svelte";
  import DiffView from "./DiffView.svelte";

  interface Props {
    provider: string;
    platformHost?: string | undefined;
    owner: string;
    name: string;
    repoPath: string;
    number: number;
  }

  const {
    provider,
    platformHost,
    owner,
    name,
    repoPath,
    number,
  }: Props = $props();

  const storageKey = "diff-file-tree-width";
  const defaultFileTreeWidth = 280;
  const minFileTreeWidth = 200;
  const maxFileTreeWidth = 520;
  let fileTreeResizeStartWidth = 0;

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

  function clampFileTreeWidth(width: number): number {
    return Math.max(
      minFileTreeWidth,
      Math.min(maxFileTreeWidth, Math.round(width)),
    );
  }

  function loadFileTreeWidth(): number {
    const raw = Number.parseInt(safeGetItem(storageKey) ?? "", 10);
    if (!Number.isFinite(raw)) return defaultFileTreeWidth;
    return clampFileTreeWidth(raw);
  }

  let fileTreeWidth = $state(loadFileTreeWidth());

  function saveFileTreeWidth(width: number): void {
    safeSetItem(storageKey, String(width));
  }

  function handleFileTreeResizeStart(): void {
    fileTreeResizeStartWidth = fileTreeWidth;
  }

  function widthFromResize(event: SplitResizeEvent): number {
    return clampFileTreeWidth(
      fileTreeResizeStartWidth + event.deltaX,
    );
  }

  function handleFileTreeResize(event: SplitResizeEvent): void {
    fileTreeWidth = widthFromResize(event);
  }

  function handleFileTreeResizeEnd(event: SplitResizeEvent): void {
    const finalWidth = widthFromResize(event);
    fileTreeWidth = finalWidth;
    saveFileTreeWidth(finalWidth);
  }

  onDestroy(() => {
    saveFileTreeWidth(fileTreeWidth);
  });
</script>

<div class="files-view">
  <DiffToolbar />
  <div class="files-layout">
    <aside
      class="files-sidebar"
      aria-label="Changed files"
      style:--diff-file-tree-width={`${fileTreeWidth}px`}
    >
      <DiffSidebar />
    </aside>
    <SplitResizeHandle
      class="files-resize-handle"
      ariaLabel="Resize file tree"
      onResizeStart={handleFileTreeResizeStart}
      onResize={handleFileTreeResize}
      onResizeEnd={handleFileTreeResizeEnd}
    />
    <div class="files-main">
      <DiffView {provider} {platformHost} {owner} {name} {repoPath} {number} />
    </div>
  </div>
</div>

<style>
  .files-view {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-height: 0;
    overflow: hidden;
  }

  .files-layout {
    display: flex;
    flex: 1;
    min-height: 0;
    overflow: hidden;
  }

  .files-sidebar {
    width: var(--diff-file-tree-width, 280px);
    flex-shrink: 0;
    border-right: 1px solid var(--border-default);
    background: var(--bg-surface);
    overflow-y: auto;
    display: flex;
    flex-direction: column;
  }

  .files-main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  @media (max-width: 720px) {
    .files-layout {
      flex-direction: column;
    }

    .files-sidebar {
      width: 100%;
      max-height: 35vh;
      border-right: none;
      border-bottom: 1px solid var(--border-default);
    }

    :global(.files-resize-handle) {
      display: none;
    }

    .files-main {
      flex: 1;
      min-height: 0;
    }
  }
</style>
