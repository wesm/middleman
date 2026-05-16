<script lang="ts">
  import type { DiffFile } from "../../api/types.js";
  import { getStores } from "../../context.js";
  import CommitListSection from "./CommitListSection.svelte";

  // Reusable file-tree + commit-list panel for the diff Files view.
  // Used by PullList (inlined under the selected PR row in the
  // standalone PR browser) and by PullDetail (as the left pane of
  // the Files tab inside the activity/kanban drawers).
  const { diff, pulls } = getStores();

  interface Props {
    showCommits?: boolean;
    resetKey?: string;
  }

  const { showCommits = true, resetKey = "" }: Props = $props();

  function filename(path: string): string {
    const i = path.lastIndexOf("/");
    return i >= 0 ? path.slice(i + 1) : path;
  }

  interface FileGroup { dir: string; files: DiffFile[] }

  function groupByDir(files: DiffFile[]): FileGroup[] {
    // Group all files with the same directory together regardless of
    // input order — API can return files in diff order, not path-sorted.
    const map = new Map<string, DiffFile[]>();
    for (const f of files) {
      const i = f.path.lastIndexOf("/");
      const dir = i > 0 ? f.path.slice(0, i) : "";
      const bucket = map.get(dir);
      if (bucket) bucket.push(f);
      else map.set(dir, [f]);
    }
    const result: FileGroup[] = [];
    for (const [dir, dirFiles] of map) {
      result.push({ dir, files: dirFiles });
    }
    return result;
  }

  function statusLetter(s: string): string {
    switch (s) {
      case "modified": return "M";
      case "added": return "A";
      case "deleted": return "D";
      case "renamed": return "R";
      case "copied": return "C";
      default: return "?";
    }
  }

  function statusColor(s: string): string {
    switch (s) {
      case "modified": return "var(--accent-amber)";
      case "added": return "var(--accent-green)";
      case "deleted": return "var(--accent-red)";
      case "renamed":
      case "copied": return "var(--accent-blue)";
      default: return "var(--text-muted)";
    }
  }

  function handleFileRowClick(path: string): void {
    const selection = window.getSelection();
    if (selection && !selection.isCollapsed) return;
    diff.requestScrollToFile(path);
  }

  function handleFileRowKeydown(event: KeyboardEvent, path: string): void {
    if (event.key !== "Enter" && event.key !== " ") return;
    event.preventDefault();
    diff.requestScrollToFile(path);
  }

  // Per-diff file filter input (shown when 10+ files in diff).
  let fileFilterText = $state("");
  // Reset filter whenever selected PR changes so stale filter text
  // does not silently hide files in the next PR.
  $effect(() => {
    const key = resetKey;
    if (showCommits) {
      pulls.getSelectedPR();
    }
    if (key === "\0") return;
    fileFilterText = "";
  });
  const showFileFilter = $derived(
    (diff.getVisibleFileList()?.files.length ?? 0) >= 10,
  );
  const filteredDiffFiles = $derived.by(() => {
    const list = diff.getVisibleFileList();
    if (!list) return null;
    // Only apply filter when the filter UI is visible to avoid
    // silent hiding when the next PR has fewer files.
    if (!showFileFilter) return list.files;
    const q = fileFilterText.trim().toLowerCase();
    if (!q) return list.files;
    return list.files.filter((f) => f.path.toLowerCase().includes(q));
  });
</script>

{#if showCommits}
  <CommitListSection />
{/if}
<div class="diff-files">
  {#if diff.isFileListLoading() && !diff.getFileList()}
    <div class="diff-files-state diff-files-state--loading">Loading files</div>
  {:else if filteredDiffFiles}
    {#if showFileFilter}
      <div class="diff-files-filter">
        <input
          type="text"
          class="diff-files-filter__input"
          placeholder="Filter files..."
          bind:value={fileFilterText}
        />
      </div>
    {/if}
    {@const grouped = groupByDir(filteredDiffFiles)}
    {#each grouped as group, gi (gi)}
      {#if group.dir}
        <div class="diff-dir-header">{group.dir}/</div>
      {/if}
      {#each group.files as f (f.path)}
        <div
          class="diff-file-row"
          class:diff-file-row--active={diff.getActiveFile() === f.path}
          class:diff-file-row--nested={!!group.dir}
          role="button"
          tabindex="0"
          onclick={() => handleFileRowClick(f.path)}
          onkeydown={(event) => handleFileRowKeydown(event, f.path)}
          title={f.path}
        >
          <span class="diff-file-status" style="color: {statusColor(f.status)}">{statusLetter(f.status)}</span>
          <span class="diff-file-name" class:diff-file-name--deleted={f.status === "deleted"}>{filename(f.path)}</span>
        </div>
      {/each}
    {/each}
  {/if}
</div>

<style>
  .diff-files {
    border-bottom: 1px solid var(--border-muted);
    padding: 4px 0;
    overflow-y: auto;
  }

  .diff-files-filter {
    padding: 4px 10px 6px 24px;
  }

  .diff-files-filter__input {
    width: 100%;
    font-size: var(--font-size-xs);
    padding: 3px 8px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-muted);
    background: var(--bg-inset);
    color: var(--text-primary);
  }

  .diff-files-filter__input:focus {
    border-color: var(--accent-blue);
    outline: none;
  }

  .diff-files-state {
    padding: 6px 24px;
    font-size: var(--font-size-xs);
    color: var(--text-muted);
  }

  .diff-files-state--loading {
    animation: pulse 1.5s ease-in-out infinite;
  }

  @keyframes pulse {
    0%, 100% { opacity: 0.4; }
    50% { opacity: 1; }
  }

  .diff-dir-header {
    padding: 5px 12px 2px 24px;
    font-family: var(--font-mono);
    font-size: var(--font-size-2xs);
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .diff-file-row {
    display: flex;
    align-items: center;
    gap: 5px;
    width: 100%;
    padding: 2px 12px 2px 24px;
    text-align: left;
    color: var(--text-secondary);
    transition: background 0.15s ease;
    cursor: pointer;
  }

  .diff-file-row--nested {
    padding-left: 36px;
  }

  .diff-file-row:hover {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  .diff-file-row--active {
    background: color-mix(in srgb, var(--accent-blue) 10%, transparent);
    color: var(--text-primary);
  }

  .diff-file-status {
    font-family: var(--font-mono);
    font-size: var(--font-size-2xs);
    font-weight: 700;
    width: 12px;
    flex-shrink: 0;
    text-align: center;
  }

  .diff-file-name {
    font-family: var(--font-mono);
    font-size: var(--font-size-xs);
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    user-select: text;
  }

  .diff-file-name--deleted {
    text-decoration: line-through;
    opacity: 0.7;
  }
</style>
