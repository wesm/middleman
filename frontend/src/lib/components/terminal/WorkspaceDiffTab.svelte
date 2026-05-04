<script lang="ts">
  import {
    DiffSidebar,
    DiffToolbar,
    DiffView,
    getStores,
  } from "@middleman/ui";
  import type { WorkspaceDiffBase } from "@middleman/ui/stores/diff";

  interface WorkspaceDiffWorkspace {
    id: string;
    repo_owner: string;
    repo_name: string;
    item_number: number;
  }

  interface Props {
    workspace: WorkspaceDiffWorkspace;
    active?: boolean;
  }

  const { workspace, active = false }: Props = $props();
  const { diff } = getStores();

  let base = $state<WorkspaceDiffBase>("head");
  const resetKey = $derived(`${workspace.id}:${base}`);

  $effect(() => {
    if (!active) return;
    void diff.loadWorkspaceDiff(workspace.id, base);
  });

  function selectBase(nextBase: WorkspaceDiffBase): void {
    base = nextBase;
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
        onclick={() => selectBase("head")}
      >
        HEAD
      </button>
      <button
        class="scope-btn"
        class:scope-btn--active={base === "origin"}
        aria-pressed={base === "origin"}
        onclick={() => selectBase("origin")}
      >
        Origin
      </button>
    </div>
  </div>
  <DiffToolbar />
  <div class="workspace-diff-layout">
    <aside class="workspace-diff-sidebar">
      <DiffSidebar showCommits={false} {resetKey} />
    </aside>
    <div class="workspace-diff-main">
      <DiffView
        owner={workspace.repo_owner}
        name={workspace.repo_name}
        number={workspace.item_number}
        loadOnMount={false}
      />
    </div>
  </div>
</section>

<style>
  .workspace-diff {
    display: flex;
    flex-direction: column;
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

  .scope-label {
    color: var(--text-secondary);
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }

  .scope-toggle {
    display: inline-flex;
    padding: 2px;
    border: 1px solid var(--border-muted);
    border-radius: 4px;
    background: var(--bg-inset);
  }

  .scope-btn {
    min-width: 58px;
    height: 20px;
    padding: 0 8px;
    border: 0;
    border-radius: 3px;
    background: transparent;
    color: var(--text-muted);
    font-size: 11px;
    font-weight: 600;
    cursor: pointer;
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
    width: 280px;
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

  @media (max-width: 720px) {
    .workspace-diff-layout {
      flex-direction: column;
    }

    .workspace-diff-sidebar {
      width: 100%;
      max-height: 35vh;
      border-right: none;
      border-bottom: 1px solid var(--border-default);
    }
  }
</style>
