<script lang="ts">
  import type { Snippet } from "svelte";
  import type {
    WorkspaceData,
    WorkspaceDetailContext,
    WorkspaceProject,
    WorkspaceWorktree,
  } from "../api/types.js";
  import CollapsibleResizableSidebar from "../components/shared/CollapsibleResizableSidebar.svelte";
  import WorkspaceSidebar from "../components/workspace/WorkspaceSidebar.svelte";

  interface Props {
    workspaceData: WorkspaceData | undefined;
    hoverCardsEnabled?: boolean;
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
    hoverCardsEnabled = false,
    onCommand,
    detailSnippet,
    sidebarWidth,
    onSidebarResize,
  }: Props = $props();

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
</script>

{#if workspaceData}
  {@const resolvedWorkspaceData = workspaceData}
  <CollapsibleResizableSidebar
    sidebarWidth={sidebarWidth ?? 280}
    onSidebarResize={onSidebarResize}
    sidebarOnly={detailSnippet == null}
    hasMain={detailSnippet != null}
    mainOverflow="hidden"
  >
    {#snippet sidebar()}
      <WorkspaceSidebar
        workspaceData={resolvedWorkspaceData}
        {hoverCardsEnabled}
        {onCommand}
      />
    {/snippet}

    {#if detailSnippet}
      <div class="detail-pane">
        {#key snippetKey}
          {@render detailSnippet(detailContext)}
        {/key}
      </div>
    {/if}
  </CollapsibleResizableSidebar>
{:else}
  <div class="workspaces-empty">
    <p>No workspace data available.</p>
  </div>
{/if}

<style>
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
