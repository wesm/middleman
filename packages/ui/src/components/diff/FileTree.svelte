<script lang="ts">
  import type { DiffFile } from "../../api/types.js";

  interface Props {
    files: DiffFile[];
    activeFile: string | null;
    whitespaceOnlyCount: number;
    hideWhitespace: boolean;
    onselect: (path: string) => void;
  }

  const { files, activeFile, whitespaceOnlyCount, hideWhitespace, onselect }: Props = $props();

  let sidebarCollapsed = $state(false);
  let filterText = $state("");
  function safeGetItem(key: string): string | null {
    try { return localStorage.getItem(key); } catch { return null; }
  }
  function safeSetItem(key: string, value: string): void {
    try { localStorage.setItem(key, value); } catch { /* ignore */ }
  }

  let sidebarWidth = $state((() => {
    const parsed = parseInt(safeGetItem("diff-sidebar-width") ?? "280", 10);
    return Number.isFinite(parsed) && parsed >= 180 && parsed <= 500 ? parsed : 280;
  })());
  let dragging = $state(false);

  // Group files by directory.
  interface FileGroup {
    dir: string;
    files: DiffFile[];
  }

  const filteredFiles = $derived(
    filterText
      ? files.filter((f) => f.path.toLowerCase().includes(filterText.toLowerCase()))
      : files,
  );

  const groups = $derived.by((): FileGroup[] => {
    const map = new Map<string, DiffFile[]>();
    for (const f of filteredFiles) {
      const lastSlash = f.path.lastIndexOf("/");
      const dir = lastSlash > 0 ? f.path.slice(0, lastSlash) : "";
      if (!map.has(dir)) map.set(dir, []);
      map.get(dir)!.push(f);
    }
    const result: FileGroup[] = [];
    for (const [dir, dirFiles] of map) {
      result.push({ dir, files: dirFiles });
    }
    return result;
  });

  function statusBadge(status: string): string {
    switch (status) {
      case "modified": return "M";
      case "added": return "A";
      case "deleted": return "D";
      case "renamed": return "R";
      case "copied": return "C";
      default: return "?";
    }
  }

  function statusClass(status: string): string {
    switch (status) {
      case "modified": return "badge--amber";
      case "added": return "badge--green";
      case "deleted": return "badge--red";
      case "renamed":
      case "copied": return "badge--blue";
      default: return "badge--muted";
    }
  }

  function filename(path: string): string {
    const lastSlash = path.lastIndexOf("/");
    return lastSlash >= 0 ? path.slice(lastSlash + 1) : path;
  }

  let fileTreeEl: HTMLDivElement | undefined = $state();

  // Drag handle for resizing. Applies width directly to the DOM during drag
  // to avoid Svelte re-renders on every pixel. State syncs once on mouseup.
  function startDrag(e: MouseEvent): void {
    e.preventDefault();
    dragging = true;
    const startX = e.clientX;
    const startWidth = sidebarWidth;

    function onMove(ev: MouseEvent): void {
      const newWidth = Math.max(180, Math.min(500, startWidth + ev.clientX - startX));
      if (fileTreeEl) fileTreeEl.style.width = `${newWidth}px`;
    }

    function onUp(ev: MouseEvent): void {
      dragging = false;
      const finalWidth = Math.max(180, Math.min(500, startWidth + ev.clientX - startX));
      sidebarWidth = finalWidth;
      safeSetItem("diff-sidebar-width", String(finalWidth));
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    }

    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
  }
</script>

{#if sidebarCollapsed}
  <div class="sidebar-collapsed">
    <button class="expand-btn" onclick={() => { sidebarCollapsed = false; }} title="Show file tree">
      <span class="expand-icon">&#9656;</span>
    </button>
  </div>
{:else}
  <div class="file-tree" bind:this={fileTreeEl} style="width: {sidebarWidth}px">
    <div class="tree-header">
      <span class="tree-title">Files</span>
      <button class="collapse-btn" onclick={() => { sidebarCollapsed = true; }} title="Hide file tree">
        <span class="collapse-icon">&#9666;</span>
      </button>
    </div>
    <div class="tree-filter">
      <input
        type="text"
        class="filter-input"
        placeholder="Filter files..."
        bind:value={filterText}
      />
    </div>
    <div class="tree-list">
      {#each groups as group}
        {#if group.dir}
          <div class="dir-header">{group.dir}/</div>
        {/if}
        {#each group.files as f}
          <button
            class="tree-file"
            class:tree-file--active={activeFile === f.path}
            onclick={() => onselect(f.path)}
          >
            <span class="file-badge {statusClass(f.status)}">{statusBadge(f.status)}</span>
            <span
              class="file-name"
              class:file-name--deleted={f.status === "deleted"}
            >{filename(f.path)}</span>
            <span class="file-counts">
              {#if f.additions > 0}
                <span class="count count--add">+{f.additions}</span>
              {/if}
              {#if f.deletions > 0}
                <span class="count count--del">-{f.deletions}</span>
              {/if}
            </span>
          </button>
        {/each}
      {/each}
    </div>
    {#if hideWhitespace && whitespaceOnlyCount > 0}
      <div class="tree-footer">
        {whitespaceOnlyCount} whitespace-only {whitespaceOnlyCount === 1 ? "file" : "files"} hidden
      </div>
    {/if}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="drag-handle"
      class:drag-handle--active={dragging}
      onmousedown={startDrag}
    ></div>
  </div>
{/if}

<style>
  .file-tree {
    position: relative;
    flex-shrink: 0;
    display: flex;
    flex-direction: column;
    background: var(--diff-sidebar-bg);
    border-right: 1px solid var(--diff-border);
    overflow: hidden;
    min-width: 180px;
    max-width: 500px;
  }


  .sidebar-collapsed {
    flex-shrink: 0;
    display: flex;
    align-items: flex-start;
    background: var(--diff-sidebar-bg);
    border-right: 1px solid var(--diff-border);
    padding: 8px 4px;
  }

  .expand-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    border-radius: var(--radius-sm);
    color: var(--text-secondary);
  }

  .expand-btn:hover {
    background: var(--bg-surface-hover);
  }

  .expand-icon,
  .collapse-icon {
    font-size: 12px;
  }

  .tree-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 12px;
    flex-shrink: 0;
  }

  .tree-title {
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-muted);
  }

  .collapse-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    border-radius: var(--radius-sm);
    color: var(--text-secondary);
  }

  .collapse-btn:hover {
    background: var(--bg-surface-hover);
  }

  .tree-filter {
    padding: 0 8px 8px;
    flex-shrink: 0;
  }

  .filter-input {
    width: 100%;
    font-size: 11px;
    padding: 4px 8px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--diff-border);
    background: var(--diff-bg);
    color: var(--diff-text);
  }

  .tree-list {
    flex: 1;
    overflow-y: auto;
    padding-bottom: 8px;
  }

  .dir-header {
    padding: 6px 12px 2px;
    font-size: 10px;
    font-weight: 600;
    color: var(--text-muted);
    text-transform: none;
    letter-spacing: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }


  .tree-file {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    padding: 3px 12px;
    text-align: left;
    font-size: 12px;
    color: var(--diff-text);
    transition: background 0.08s;
    border-left: 2px solid transparent;
  }


  .tree-file:hover {
    background: var(--bg-surface-hover);
  }

  .tree-file--active {
    background: var(--diff-sidebar-active);
    border-left-color: var(--diff-sidebar-accent);
  }

  .file-badge {
    font-size: 10px;
    font-weight: 700;
    width: 16px;
    height: 16px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 3px;
    flex-shrink: 0;
  }

  .badge--amber {
    color: var(--accent-amber);
    background: color-mix(in srgb, var(--accent-amber) 15%, transparent);
  }

  .badge--green {
    color: var(--accent-green);
    background: color-mix(in srgb, var(--accent-green) 15%, transparent);
  }

  .badge--red {
    color: var(--accent-red);
    background: color-mix(in srgb, var(--accent-red) 15%, transparent);
  }

  .badge--blue {
    color: var(--accent-blue);
    background: color-mix(in srgb, var(--accent-blue) 15%, transparent);
  }

  .badge--muted {
    color: var(--text-muted);
    background: var(--bg-inset);
  }

  .file-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--font-mono);
    font-size: 11px;
  }

  .file-name--deleted {
    text-decoration: line-through;
  }

  .file-counts {
    display: flex;
    gap: 4px;
    flex-shrink: 0;
    min-width: 48px;
    justify-content: flex-end;
  }

  .count {
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 600;
    min-width: 22px;
    text-align: right;
  }

  .count--add {
    color: var(--diff-add-text);
  }

  .count--del {
    color: var(--diff-del-text);
  }

  .tree-footer {
    padding: 8px 12px;
    font-size: 11px;
    color: var(--text-muted);
    border-top: 1px solid var(--diff-border);
    flex-shrink: 0;
  }

  .drag-handle {
    position: absolute;
    top: 0;
    right: -2px;
    width: 4px;
    height: 100%;
    cursor: col-resize;
    z-index: 3;
  }

  .drag-handle:hover,
  .drag-handle--active {
    background: var(--accent-blue);
  }
</style>
