<script lang="ts">
  import type { Snippet } from "svelte";

  interface Props {
    children?: Snippet | undefined;
    sidebar: Snippet;
    trailing?: Snippet | undefined;
    isCollapsed?: boolean;
    hideSidebar?: boolean;
    sidebarWidth?: number;
    sidebarOnly?: boolean;
    hasMain?: boolean;
    showCollapsedStrip?: boolean;
    mainEmpty?: boolean;
    mainOverflow?: "auto" | "hidden";
    minSidebarWidth?: number;
    maxSidebarWidth?: number;
    onSidebarResize?: ((width: number) => void) | undefined;
    onExpand?: (() => void) | undefined;
  }

  let {
    children = undefined,
    sidebar,
    trailing = undefined,
    isCollapsed = false,
    hideSidebar = false,
    sidebarWidth = 340,
    sidebarOnly = false,
    hasMain = true,
    showCollapsedStrip = false,
    mainEmpty = false,
    mainOverflow = "auto",
    minSidebarWidth = 200,
    maxSidebarWidth = 600,
    onSidebarResize = undefined,
    onExpand = undefined,
  }: Props = $props();

  // svelte-ignore state_referenced_locally
  // eslint-disable-next-line svelte/prefer-writable-derived -- $derived.writable not in svelte 5.55
  let committedWidth = $state(sidebarWidth);
  $effect(() => { committedWidth = sidebarWidth; });
  let dragWidth: number | null = $state(null);
  let currentWidth = $derived(dragWidth ?? committedWidth);

  function startResize(event: MouseEvent): void {
    event.preventDefault();
    const startX = event.clientX;
    const startWidth = currentWidth;

    function onMove(moveEvent: MouseEvent): void {
      dragWidth = Math.max(
        minSidebarWidth,
        Math.min(
          maxSidebarWidth,
          startWidth + moveEvent.clientX - startX,
        ),
      );
    }

    function onUp(): void {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
      const finalWidth = currentWidth;
      onSidebarResize?.(finalWidth);
      committedWidth = finalWidth;
      dragWidth = null;
    }

    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
  }
</script>

<div class="list-layout">
  {#if !isCollapsed && !hideSidebar}
    <aside
      class="sidebar"
      style={`width: ${sidebarOnly || !hasMain ? "100%" : `${currentWidth}px`}`}
    >
      {@render sidebar()}
    </aside>
    {#if !sidebarOnly && hasMain}
      <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
      <div
        class="resize-handle"
        role="separator"
        aria-label="Resize sidebar"
        aria-orientation="vertical"
        onmousedown={startResize}
      ></div>
    {/if}
  {:else if !hideSidebar && showCollapsedStrip}
    <aside class="sidebar sidebar--collapsed">
      <button class="expand-btn" onclick={onExpand} title="Expand sidebar">
        <svg width="14" height="14" viewBox="0 0 16 16"
          fill="none" stroke="currentColor" stroke-width="1.5">
          <rect x="1" y="1" width="14" height="14" rx="2" />
          <line x1="6" y1="1" x2="6" y2="15" />
          <polyline points="8,6 10,8 8,10"
            stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>
    </aside>
  {/if}

  {#if hasMain}
    <section
      class="main-area"
      class:main-area--empty={mainEmpty}
      class:main-area--hidden={mainOverflow === "hidden"}
    >
      {#if children}
        {@render children()}
      {/if}
    </section>
  {/if}

  {#if trailing}
    {@render trailing()}
  {/if}
</div>

<style>
  .list-layout {
    display: flex;
    flex: 1;
    overflow: hidden;
  }

  .sidebar {
    width: 340px;
    flex-shrink: 0;
    background: var(--bg-surface);
    border-right: 1px solid var(--border-default);
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .sidebar--collapsed {
    width: 28px;
    align-items: center;
    padding-top: 6px;
  }

  .expand-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    cursor: pointer;
    transition: color 0.1s, background 0.1s;
  }

  .expand-btn:hover {
    color: var(--text-primary);
    background: var(--bg-surface-hover);
  }

  .resize-handle {
    width: 4px;
    cursor: col-resize;
    background: var(--border-muted);
    flex-shrink: 0;
  }

  .resize-handle:hover {
    background: var(--accent-blue);
  }

  .main-area {
    flex: 1;
    min-width: 0;
    overflow-y: auto;
    background: var(--bg-primary);
    display: flex;
    flex-direction: column;
  }

  .main-area--empty {
    align-items: center;
    justify-content: center;
  }

  .main-area--hidden {
    overflow: hidden;
  }
</style>
