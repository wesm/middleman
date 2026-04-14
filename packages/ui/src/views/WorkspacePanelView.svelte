<script lang="ts">
  import { getStores } from "../context.js";
  import type { PullRequest } from "../api/types.js";
  import type { FetchPullResult } from "../stores/pulls.svelte.js";
  import WorkspacePanelPRItem
    from "../components/workspace/WorkspacePanelPRItem.svelte";

  const { pulls } = getStores();

  interface Props {
    view: "list" | "detail" | "empty";
    isPinned?: boolean | undefined;
    platformHost?: string | undefined;
    owner?: string | undefined;
    name?: string | undefined;
    number?: number | undefined;
    emptyReason?: string | undefined;
    activePlatformHost?: string | null | undefined;
    onSelectPR: (number: number) => void;
    onBack: () => void;
    onCreateWorktree: (number: number) => void;
    onNavigateWorktree: (worktreeKey: string) => void;
    onUnpin?: (() => void) | undefined;
    onRefresh?: () => void;
    onRevealHostSettings?: () => void;
  }

  let {
    view,
    isPinned = false,
    platformHost,
    owner,
    name,
    number,
    emptyReason,
    activePlatformHost = null,
    onSelectPR,
    onBack,
    onCreateWorktree,
    onNavigateWorktree,
    onUnpin,
    onRefresh,
    onRevealHostSettings,
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

  let fetchResult = $state<FetchPullResult | null>(null);
  let fetchingPull = $state(false);
  let lastFetchKey = $state<string | null>(null);

  function fetchKey(): string | null {
    if (!platformHost || !owner || !name || !number) {
      return null;
    }
    return `${platformHost}/${owner}/${name}/${number}`;
  }

  $effect(() => {
    const key = fetchKey();
    if (view !== "detail" || !key || !number || !owner || !name) {
      fetchResult = null;
      lastFetchKey = null;
      return;
    }
    if (selectedPull) return;
    if (key === lastFetchKey) return;

    lastFetchKey = key;
    fetchingPull = true;
    const capturedKey = key;
    pulls.fetchSinglePull(owner, name, number).then((r) => {
      if (capturedKey === fetchKey()) {
        fetchResult = r;
        fetchingPull = false;
      }
    });
  });

  const fetchedPull = $derived(
    fetchResult?.status === "found" ? fetchResult.pull : null,
  );
  const fetchError = $derived(
    fetchResult?.status === "error" ? fetchResult.message : null,
  );
  const resolvedPull = $derived(selectedPull ?? fetchedPull);

  function handleDetailRefresh(): void {
    lastFetchKey = null;
    fetchResult = null;
    onRefresh?.();
  }
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
        This worktree's repository is on
        <strong>{platformHost}</strong>.
      </p>
      <p class="panel-muted">
        Pull request data is only available for repositories
        on the active host
        ({activePlatformHost}).
      </p>
      {#if onRevealHostSettings}
        <button
          class="panel-action-btn"
          onclick={onRevealHostSettings}
        >Reveal in Host Settings</button>
      {/if}
    </div>
  {:else if view === "detail"}
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
        {#if isPinned}
          <button
            class="panel-unpin-btn"
            onclick={onUnpin}
            title="Unpin and follow selection"
          >Unpin</button>
        {/if}
        {#if resolvedPull}
          <span class="detail-title">
            #{resolvedPull.Number} {resolvedPull.Title}
          </span>
        {:else if number}
          <span class="detail-title">PR #{number}</span>
        {/if}
      </div>
      <div class="detail-body">
        {#if resolvedPull}
          {#if resolvedPull.Body}
            <p class="body-text">{resolvedPull.Body}</p>
          {:else}
            <p class="body-empty">No description provided.</p>
          {/if}
        {:else if fetchingPull}
          <div class="detail-body-empty" data-testid="detail-loading">
            <p>Loading PR #{number}...</p>
          </div>
        {:else if fetchError}
          <div class="detail-body-empty" data-testid="detail-error">
            <p>Failed to load PR #{number}.</p>
            <p class="error-hint">{fetchError}</p>
            {#if onRefresh}
              <button
                class="panel-action-btn"
                onclick={handleDetailRefresh}
              >Retry</button>
            {/if}
          </div>
        {:else}
          <div class="detail-body-empty" data-testid="detail-not-found">
            <p>PR #{number} was not found.</p>
            {#if onRefresh}
              <button
                class="panel-action-btn"
                onclick={handleDetailRefresh}
              >Refresh</button>
            {/if}
          </div>
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
          <p class="panel-empty-text">
            Loading pull requests...
          </p>
        {:else if filteredPulls.length === 0}
          <div class="panel-empty-state">
            <p>No open pull requests for {owner}/{name}.</p>
            {#if onRefresh}
              <button
                class="panel-action-btn"
                onclick={onRefresh}
              >Refresh</button>
            {/if}
          </div>
        {:else}
          {#each filteredPulls as pr (pr.ID)}
            <WorkspacePanelPRItem
              pull={pr}
              onSelect={onSelectPR}
              {onCreateWorktree}
              {onNavigateWorktree}
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
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 4px;
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

  .panel-unpin-btn {
    font-size: 11px;
    padding: 2px 6px;
    border: 1px solid var(--border-muted);
    border-radius: 4px;
    background: var(--bg-inset);
    color: var(--text-muted);
    cursor: pointer;
    flex-shrink: 0;
  }

  .panel-unpin-btn:hover {
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
    display: flex;
    flex-direction: column;
  }

  .detail-body-empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 4px;
    flex: 1;
    color: var(--text-muted);
    font-size: 13px;
    text-align: center;
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

  .error-hint {
    font-size: 11px;
    color: var(--accent-red);
    margin-top: 2px;
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

  .panel-empty-state {
    padding: 24px 16px;
    font-size: 13px;
    color: var(--text-muted);
    text-align: center;
  }

  .panel-muted {
    font-size: 12px;
    color: var(--text-muted);
    margin-top: 4px;
  }

  .panel-action-btn {
    display: inline-block;
    margin-top: 8px;
    padding: 4px 12px;
    font-size: 12px;
    font-weight: 500;
    color: var(--text-secondary);
    background: var(--bg-surface);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm, 4px);
    cursor: pointer;
  }

  .panel-action-btn:hover {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }
</style>
