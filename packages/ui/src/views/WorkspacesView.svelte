<script lang="ts">
  import WorkspaceSidebar from "../components/workspace/WorkspaceSidebar.svelte";
  import type {
    WorkspaceData,
    WorkspaceDetailContext,
    WorkspaceProject,
    WorkspaceWorktree,
  } from "../api/types.js";
  import type { Snippet } from "svelte";

  interface Props {
    workspaceData: WorkspaceData | undefined;
    onCommand: (
      command: string,
      payload: Record<string, unknown>,
    ) => void;
    detailSnippet?: Snippet<[WorkspaceDetailContext]>;
    sidebarWidth?: number | undefined;
    onSidebarResize?: (width: number) => void;
  }

  let {
    workspaceData,
    onCommand,
    detailSnippet,
    sidebarWidth,
    onSidebarResize,
  }: Props = $props();

  let currentWidth = $state(280);

  $effect(() => {
    if (sidebarWidth != null) {
      currentWidth = sidebarWidth;
    }
  });

  const detailContext: WorkspaceDetailContext = $derived.by(
    () => {
      if (!workspaceData) {
        return { worktree: null, project: null, host: null };
      }
      const host =
        workspaceData.hosts.find(
          (h) => h.key === workspaceData.selectedHostKey,
        ) ?? workspaceData.hosts[0] ?? null;
      let project: WorkspaceProject | null = null;
      let worktree: WorkspaceWorktree | null = null;
      if (host && workspaceData.selectedWorktreeKey) {
        for (const p of host.projects) {
          const wt = p.worktrees.find(
            (w) =>
              w.key === workspaceData.selectedWorktreeKey,
          );
          if (wt) {
            project = p;
            worktree = wt;
            break;
          }
        }
      }
      return { worktree, project, host };
    },
  );

  const snippetKey = $derived(
    (detailContext.host?.key ?? "") +
      "/" +
      (detailContext.project?.key ?? "") +
      "/" +
      (detailContext.worktree?.key ?? ""),
  );

  function startResize(e: MouseEvent): void {
    e.preventDefault();
    const startX = e.clientX;
    const startW = currentWidth;

    function onMove(ev: MouseEvent): void {
      currentWidth = Math.max(
        200,
        Math.min(600, startW + ev.clientX - startX),
      );
    }

    function onUp(): void {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
      onSidebarResize?.(currentWidth);
    }

    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
  }
</script>

<div class="workspaces-view">
  {#if workspaceData}
    <div
      class="sidebar-pane"
      style="width: {detailSnippet
        ? currentWidth + 'px'
        : '100%'}"
    >
      <WorkspaceSidebar {workspaceData} {onCommand} />
    </div>

    {#if detailSnippet}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="resize-handle"
        onmousedown={startResize}
      ></div>
      <div class="detail-pane">
        {#key snippetKey}
          {@render detailSnippet(detailContext)}
        {/key}
      </div>
    {/if}
  {:else}
    <div class="workspaces-empty">
      <p>No workspace data available.</p>
    </div>
  {/if}
</div>

<style>
  .workspaces-view {
    flex: 1;
    display: flex;
    overflow: hidden;
    background: var(--bg-primary);
  }

  .sidebar-pane {
    flex-shrink: 0;
    overflow: hidden;
    display: flex;
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

  .detail-pane {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    display: flex;
  }

  .workspaces-empty {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--text-muted);
  }
</style>
