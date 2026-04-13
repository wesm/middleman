<script lang="ts">
  import { getStores } from "../context.js";
  import WorkspacePanelPRItem
    from "../components/workspace/WorkspacePanelPRItem.svelte";

  const { pulls } = getStores();

  interface Props {
    view: "list" | "detail" | "empty";
    platformHost?: string | undefined;
    owner?: string | undefined;
    name?: string | undefined;
    number?: number | undefined;
    emptyReason?: string | undefined;
    activePlatformHost?: string | null | undefined;
    onSelectPR: (number: number) => void;
    onBack: () => void;
    onCreateWorktree: (number: number) => void;
  }

  let {
    view,
    platformHost,
    owner,
    name,
    number,
    emptyReason,
    activePlatformHost = null,
    onSelectPR,
    onBack,
    onCreateWorktree,
  }: Props = $props();

  const filteredPulls = $derived.by(() => {
    if (view !== "list" || !owner || !name || !platformHost) return [];
    return pulls.getPulls().filter(
      (p) =>
        p.repo_owner === owner &&
        p.repo_name === name &&
        p.platform_host === platformHost,
    );
  });

  const selectedPull = $derived.by(() => {
    if (view !== "detail" || !number || !platformHost) return null;
    return pulls.getPulls().find(
      (p) =>
        p.Number === number &&
        p.repo_owner === owner &&
        p.repo_name === name &&
        p.platform_host === platformHost,
    ) ?? null;
  });

  const isNonPrimary = $derived(
    activePlatformHost !== null &&
    platformHost !== undefined &&
    platformHost !== activePlatformHost,
  );
</script>

<div class="workspace-panel">
  {#if view === "empty"}
    <div class="panel-empty">
      {#if emptyReason === "noPlatformRepo"}
        <p>This worktree has no linked repository.</p>
      {:else}
        <p>Select a worktree to see its pull requests.</p>
      {/if}
    </div>
  {:else if activePlatformHost === null}
    <div class="panel-empty">
      <p>Middleman is starting up...</p>
    </div>
  {:else if isNonPrimary}
    <div class="panel-empty" data-testid="non-primary-state">
      <p>
        This repository is on a different platform host
        ({platformHost}).
      </p>
    </div>
  {:else if view === "detail" && selectedPull}
    <div class="panel-detail">
      <div class="detail-header">
        <button class="back-btn" onclick={onBack} aria-label="Back to list">
          <svg
            width="12"
            height="12"
            viewBox="0 0 16 16"
            fill="currentColor"
          >
            <path
              d="M7.78 12.53a.75.75 0 01-1.06 0L2.47 \
8.28a.75.75 0 010-1.06l4.25-4.25a.75.75 0 011.06 \
1.06L4.81 7h7.44a.75.75 0 010 1.5H4.81l2.97 \
2.97a.75.75 0 010 1.06z"
            />
          </svg>
        </button>
        <span class="detail-title">
          #{selectedPull.Number} {selectedPull.Title}
        </span>
      </div>
      <div class="detail-body">
        {#if selectedPull.Body}
          <p class="body-text">{selectedPull.Body}</p>
        {:else}
          <p class="body-empty">No description provided.</p>
        {/if}
      </div>
    </div>
  {:else if view === "list"}
    <div class="panel-list">
      <div class="list-header">
        <span class="repo-label">{owner}/{name}</span>
        <span class="count-badge">
          {filteredPulls.length}
        </span>
      </div>
      <div class="list-body">
        {#if pulls.isLoading() && filteredPulls.length === 0}
          <p class="panel-empty-text">Loading...</p>
        {:else if filteredPulls.length === 0}
          <p class="panel-empty-text">
            No pull requests found.
          </p>
        {:else}
          {#each filteredPulls as pr (pr.ID)}
            <WorkspacePanelPRItem
              pull={pr}
              onSelect={onSelectPR}
              {onCreateWorktree}
            />
          {/each}
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .workspace-panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    width: 100%;
    background: var(--bg-primary);
  }

  .panel-empty {
    display: flex;
    align-items: center;
    justify-content: center;
    flex: 1;
    padding: 16px;
    color: var(--text-muted);
    font-size: 13px;
    text-align: center;
  }

  .panel-detail {
    display: flex;
    flex-direction: column;
    height: 100%;
  }

  .detail-header {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 10px;
    border-bottom: 1px solid var(--border-default);
    background: var(--bg-surface);
    flex-shrink: 0;
  }

  .back-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    border: none;
    border-radius: var(--radius-sm, 4px);
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    flex-shrink: 0;
  }

  .back-btn:hover {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  .detail-title {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }

  .detail-body {
    flex: 1;
    overflow-y: auto;
    padding: 12px;
  }

  .body-text {
    font-size: 13px;
    color: var(--text-secondary);
    line-height: 1.5;
    white-space: pre-wrap;
    word-break: break-word;
  }

  .body-empty {
    font-size: 13px;
    color: var(--text-muted);
    font-style: italic;
  }

  .panel-list {
    display: flex;
    flex-direction: column;
    height: 100%;
  }

  .list-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 8px 10px;
    border-bottom: 1px solid var(--border-default);
    background: var(--bg-surface);
    flex-shrink: 0;
  }

  .repo-label {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }

  .count-badge {
    font-size: 11px;
    font-weight: 600;
    color: var(--text-muted);
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    border-radius: 10px;
    padding: 2px 7px;
    flex-shrink: 0;
  }

  .list-body {
    flex: 1;
    overflow-y: auto;
  }

  .panel-empty-text {
    padding: 24px 16px;
    font-size: 13px;
    color: var(--text-muted);
    text-align: center;
  }
</style>
