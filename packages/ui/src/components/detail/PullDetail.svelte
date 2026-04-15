<script lang="ts">
  import type { KanbanStatus } from "../../api/types.js";
  import {
    getStores, getClient, getActions,
    getUIConfig, getNavigate,
  } from "../../context.js";
  import { renderMarkdown } from "../../utils/markdown.js";
  import { timeAgo } from "../../utils/time.js";
  import { copyToClipboard } from "../../utils/clipboard.js";
  import EventTimeline from "./EventTimeline.svelte";
  import CommentBox from "./CommentBox.svelte";
  import ApproveButton from "./ApproveButton.svelte";
  import ApproveWorkflowsButton from "./ApproveWorkflowsButton.svelte";
  import MergeModal from "./MergeModal.svelte";
  import ReadyForReviewButton from "./ReadyForReviewButton.svelte";
  import ActionButton from "../shared/ActionButton.svelte";
  import GitHubLabels from "../shared/GitHubLabels.svelte";
  import DiffView from "../diff/DiffView.svelte";
  import DiffSidebar from "../diff/DiffSidebar.svelte";
  import CIStatus from "./CIStatus.svelte";

  const { detail: detailStore, pulls, activity } = getStores();
  const client = getClient();
  const actions = getActions();
  const uiConfig = getUIConfig();
  const navigate = getNavigate();

  interface Props {
    owner: string;
    name: string;
    number: number;
    onPullsRefresh?: () => Promise<void>;
    hideTabs?: boolean;
  }

  const {
    owner, name, number, onPullsRefresh, hideTabs = false,
  }: Props = $props();

  let activeTab = $state<"conversation" | "files">("conversation");

  $effect(() => {
    void detailStore.loadDetail(owner, name, number);
    detailStore.startDetailPolling(owner, name, number);
    return () => detailStore.stopDetailPolling();
  });

  let copied = $state(false);
  let copyTimeout: ReturnType<typeof setTimeout> | null = null;

  function copyBody(text: string): void {
    void copyToClipboard(text).then((ok) => {
      if (!ok) return;
      copied = true;
      if (copyTimeout !== null) clearTimeout(copyTimeout);
      copyTimeout = setTimeout(() => {
        copied = false;
        copyTimeout = null;
      }, 1500);
    });
  }

  async function refreshPulls(): Promise<void> {
    if (onPullsRefresh) {
      await onPullsRefresh();
    } else {
      await pulls.loadPulls();
    }
  }

  let stateSubmitting = $state(false);
  let stateError = $state<string | null>(null);

  async function handleStateChange(
    newState: "open" | "closed",
  ): Promise<void> {
    stateSubmitting = true;
    stateError = null;
    try {
      const { error: requestError } = await client.POST(
        "/repos/{owner}/{name}/pulls/{number}/github-state",
        {
          params: { path: { owner, name, number } },
          body: { state: newState },
        },
      );
      if (requestError) {
        throw new Error(
          requestError.detail
            ?? requestError.title
            ?? "failed to change PR state",
        );
      }
      await detailStore.loadDetail(owner, name, number);
      await refreshPulls();
      await activity.loadActivity();
    } catch (err) {
      stateError =
        err instanceof Error ? err.message : String(err);
    } finally {
      stateSubmitting = false;
    }
  }

  let repoSettings = $state<{
    allowSquash: boolean;
    allowMerge: boolean;
    allowRebase: boolean;
  } | null>(null);
  let showMergeModal = $state(false);

  $effect(() => {
    client.GET("/repos/{owner}/{name}", {
      params: { path: { owner, name } },
    }).then(({ data, error }) => {
      if (error || !data) return;
      repoSettings = {
        allowSquash: data.AllowSquashMerge,
        allowMerge: data.AllowMergeCommit,
        allowRebase: data.AllowRebaseMerge,
      };
    }).catch(() => {});
  });

  const workflowApproval = $derived(
    detailStore.getDetail()?.workflow_approval,
  );

  const kanbanOptions: { value: KanbanStatus; label: string }[] = [
    { value: "new", label: "New" },
    { value: "reviewing", label: "Reviewing" },
    { value: "waiting", label: "Waiting" },
    { value: "awaiting_merge", label: "Awaiting Merge" },
  ];

  function reviewColor(decision: string): string {
    if (decision === "APPROVED") return "chip--green";
    if (decision === "CHANGES_REQUESTED") return "chip--red";
    return "chip--muted";
  }

  function onKanbanChange(e: Event): void {
    const select = e.target as HTMLSelectElement;
    void detailStore.updateKanbanState(owner, name, number, select.value as KanbanStatus);
  }

  const worktreeLinks = $derived(
    detailStore.getDetail()?.worktree_links ?? [],
  );
  const hasWorktreeLinks = $derived(
    worktreeLinks.length > 0,
  );
  const importAction = $derived(
    (actions.pull ?? []).find(
      (a) => a.id === "import-worktree",
    ),
  );
  const navigateAction = $derived(
    (actions.pull ?? []).find(
      (a) => a.id === "navigate-worktree",
    ),
  );
  const otherActions = $derived(
    (actions.pull ?? []).filter(
      (a) =>
        a.id !== "import-worktree" &&
        a.id !== "navigate-worktree",
    ),
  );
  const labels = $derived(detailStore.getDetail()?.merge_request?.labels ?? []);

  const workspace = $derived(detailStore.getDetail()?.workspace);
  let wsCreating = $state(false);
  let wsError = $state<string | null>(null);

  async function createWorkspace(): Promise<void> {
    const detail = detailStore.getDetail();
    if (!detail) return;

    wsCreating = true;
    wsError = null;
    try {
      const { data, error: reqError } = await client.POST(
        "/workspaces",
        {
          body: {
            platform_host: detail.platform_host,
            owner: detail.repo_owner,
            name: detail.repo_name,
            mr_number: detail.merge_request.Number,
          },
        },
      );
      if (reqError) {
        throw new Error(
          reqError.detail ?? reqError.title ?? "failed to create workspace",
        );
      }
      if (data?.id) {
        navigate(`/terminal/${data.id}`);
      }
    } catch (err) {
      wsError = err instanceof Error ? err.message : String(err);
    } finally {
      wsCreating = false;
    }
  }
</script>

{#if detailStore.isDetailLoading()}
  <div class="state-center"><p class="state-msg">Loading…</p></div>
{:else if detailStore.getDetailError() !== null && detailStore.getDetail() === null}
  <div class="state-center"><p class="state-msg state-msg--error">Error: {detailStore.getDetailError()}</p></div>
{:else}
  {@const detail = detailStore.getDetail()}
  {#if detail !== null}
    {@const pr = detail.merge_request}
    <div class="pull-detail-wrap">
      {#if !hideTabs}
        <div class="detail-tabs">
          <button
            type="button"
            class="detail-tab"
            class:detail-tab--active={activeTab === "conversation"}
            onclick={() => { activeTab = "conversation"; }}
          >
            Conversation
          </button>
          <button
            type="button"
            class="detail-tab"
            class:detail-tab--active={activeTab === "files"}
            onclick={() => { activeTab = "files"; }}
          >
            Files changed
            {#if pr.Additions > 0}
              <span class="files-stat files-stat--add">+{pr.Additions}</span>
            {/if}
            {#if pr.Deletions > 0}
              <span class="files-stat files-stat--del">-{pr.Deletions}</span>
            {/if}
          </button>
        </div>
      {/if}
      {#if !hideTabs && activeTab === "files"}
        <div class="files-layout">
          <aside class="files-sidebar">
            <DiffSidebar />
          </aside>
          <div class="files-main">
            <DiffView {owner} {name} {number} />
          </div>
        </div>
      {:else}
        <div class="pull-detail">
      {#if detailStore.isStaleRefreshing()}
        <div class="refresh-banner">
          <span class="sync-dot"></span>
          Refreshing...
        </div>
      {/if}
      <!-- Header -->
      <div class="detail-header">
        <h2 class="detail-title">{pr.Title}</h2>
        {#if !uiConfig.hideStar}
          <button
            class="star-btn"
            onclick={() => void detailStore.toggleDetailPRStar(owner, name, number, pr.Starred)}
            title={pr.Starred ? "Unstar" : "Star"}
          >
            {#if pr.Starred}
              <svg class="star-detail-icon star-detail-icon--active" width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
                <path d="M8 .25a.75.75 0 01.673.418l1.882 3.815 4.21.612a.75.75 0 01.416 1.279l-3.046 2.97.719 4.192a.75.75 0 01-1.088.791L8 12.347l-3.766 1.98a.75.75 0 01-1.088-.79l.72-4.194L.818 6.374a.75.75 0 01.416-1.28l4.21-.611L7.327.668A.75.75 0 018 .25z"/>
              </svg>
            {:else}
              <svg class="star-detail-icon" width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
                <path d="M8 .25a.75.75 0 01.673.418l1.882 3.815 4.21.612a.75.75 0 01.416 1.279l-3.046 2.97.719 4.192a.75.75 0 01-1.088.791L8 12.347l-3.766 1.98a.75.75 0 01-1.088-.79l.72-4.194L.818 6.374a.75.75 0 01.416-1.28l4.21-.611L7.327.668A.75.75 0 018 .25zm0 2.445L6.615 5.5a.75.75 0 01-.564.41l-3.097.45 2.24 2.184a.75.75 0 01.216.664l-.528 3.084 2.769-1.456a.75.75 0 01.698 0l2.77 1.456-.53-3.084a.75.75 0 01.216-.664l2.24-2.183-3.096-.45a.75.75 0 01-.564-.41L8 2.694z"/>
              </svg>
            {/if}
          </button>
        {/if}
        <a class="gh-link" href={pr.URL} target="_blank" rel="noopener noreferrer" title="Open on GitHub">
          <svg width="14" height="14" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
            <path d="M6 3H3a1 1 0 0 0-1 1v9a1 1 0 0 0 1 1h9a1 1 0 0 0 1-1v-3" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
            <path d="M10 2h4v4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
            <path d="M8 8L14 2" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
          </svg>
        </a>
      </div>

      <!-- Meta row -->
      <div class="meta-row">
        <span class="meta-item">{detail.repo_owner}/{detail.repo_name}</span>
        <span class="meta-sep">·</span>
        <span class="meta-item">#{pr.Number}</span>
        <span class="meta-sep">·</span>
        <span class="meta-item">{pr.Author}</span>
        <span class="meta-sep">·</span>
        <span class="meta-item">{timeAgo(pr.CreatedAt)}</span>
        {#if detailStore.isDetailSyncing()}
          <span class="meta-sep">·</span>
          <span class="sync-indicator" title="Syncing from GitHub">
            <svg class="sync-spinner" width="12" height="12" viewBox="0 0 16 16" fill="none">
              <circle cx="8" cy="8" r="6" stroke="currentColor" stroke-width="2" stroke-dasharray="28" stroke-dashoffset="8" stroke-linecap="round"/>
            </svg>
            Syncing
          </span>
        {/if}
      </div>

      <!-- Chips row -->
      <div class="chips-row">
        {#if pr.State === "merged"}
          <span class="chip chip--purple">Merged</span>
        {:else if pr.State === "closed"}
          <span class="chip chip--red">Closed</span>
        {:else if pr.IsDraft}
          <span class="chip chip--amber">Draft</span>
        {:else}
          <span class="chip chip--green">Open</span>
        {/if}
        <CIStatus
          status={pr.CIStatus}
          checksJSON={pr.CIChecksJSON}
          detailLoaded={detailStore.getDetailLoaded()}
          detailSyncing={detailStore.isDetailSyncing()}
        />
        {#if pr.ReviewDecision}
          <span class="chip {reviewColor(pr.ReviewDecision)}">{pr.ReviewDecision.replace(/_/g, " ")}</span>
        {/if}
        {#if pr.Additions > 0 || pr.Deletions > 0}
          <span class="chip chip--muted">+{pr.Additions}/-{pr.Deletions}</span>
        {/if}
        {#if hasWorktreeLinks}
          <span class="chip chip--teal">Worktree</span>
        {/if}
      </div>

      {#if labels.length > 0}
        <GitHubLabels {labels} mode="full" />
      {/if}

      <!-- Kanban state -->
      <div class="kanban-row">
        <label class="kanban-label" for="kanban-select">Status</label>
        <select
          id="kanban-select"
          class="kanban-select kanban-select--{pr.KanbanStatus.replace('_', '-')}"
          value={pr.KanbanStatus}
          onchange={onKanbanChange}
        >
          {#each kanbanOptions as opt (opt.value)}
            <option value={opt.value}>{opt.label}</option>
          {/each}
        </select>
      </div>

      <!-- Mergeable state warnings -->
      {#if pr.State === "open" && pr.MergeableState === "dirty"}
        <div class="merge-warning merge-warning--conflict">
          <span>This branch has conflicts that must be resolved before merging.</span>
          <a href={pr.URL} target="_blank" rel="noopener noreferrer">View on GitHub</a>
        </div>
      {:else if pr.State === "open" && pr.MergeableState === "blocked"}
        <div class="merge-warning merge-warning--info">
          <span>Branch protection rules may prevent this merge.</span>
        </div>
      {:else if pr.State === "open" && pr.MergeableState === "behind"}
        <div class="merge-warning merge-warning--info">
          <span>This branch is behind the base branch and may need to be updated.</span>
        </div>
      {:else if pr.State === "open" && pr.MergeableState === "unstable"}
        <div class="merge-warning merge-warning--info">
          <span>Required status checks have not passed.</span>
        </div>
      {/if}

      <!-- Diff sync warnings (stale or unavailable diff data) -->
      {#if detail.warnings && detail.warnings.length > 0}
        {#each detail.warnings as warning}
          <div class="merge-warning merge-warning--info">
            <span>{warning}</span>
          </div>
        {/each}
      {/if}

      <!-- Approve / Merge / Close / Reopen actions -->
      {#if pr.State !== "merged"}
        <div class="actions-row">
          {#if pr.State === "open"}
            {#if pr.IsDraft}
              <ReadyForReviewButton {owner} {name} {number} size="sm" />
            {/if}
            <ApproveButton {owner} {name} {number} size="sm" />
            {#if workflowApproval?.checked && workflowApproval.required}
              <ApproveWorkflowsButton
                {owner}
                {name}
                {number}
                count={workflowApproval.count ?? 0}
                size="sm"
              />
            {/if}
            {#if repoSettings}
              <ActionButton
                class="btn--merge"
                onclick={() => { showMergeModal = true; }}
                tone="success"
                surface="solid"
                size="sm"
              >
                {#if repoSettings.allowSquash && !repoSettings.allowMerge && !repoSettings.allowRebase}
                  Squash and merge
                {:else if !repoSettings.allowSquash && repoSettings.allowMerge && !repoSettings.allowRebase}
                  Merge
                {:else if !repoSettings.allowSquash && !repoSettings.allowMerge && repoSettings.allowRebase}
                  Rebase and merge
                {:else}
                  Merge &#9662;
                {/if}
              </ActionButton>
            {/if}
            <ActionButton
              class="btn--close"
              disabled={stateSubmitting}
              onclick={() => handleStateChange("closed")}
              tone="danger"
              surface="outline"
              size="sm"
            >
              {stateSubmitting ? "Closing..." : "Close"}
            </ActionButton>
          {:else if pr.State === "closed"}
            <ActionButton
              class="btn--reopen"
              disabled={stateSubmitting}
              onclick={() => handleStateChange("open")}
              tone="success"
              surface="solid"
              size="sm"
            >
              {stateSubmitting ? "Reopening..." : "Reopen"}
            </ActionButton>
          {/if}
          {#if stateError}
            <span class="action-error">{stateError}</span>
          {/if}
        </div>
      {/if}

      <!-- Workspace actions -->
      <div class="actions-row">
        {#if workspace}
          <button
            class="btn--workspace"
            onclick={() => navigate(`/terminal/${workspace.id}`)}
          >
            Open Workspace
          </button>
        {:else}
          <button
            class="btn--workspace"
            disabled={wsCreating}
            onclick={() => void createWorkspace()}
          >
            {wsCreating ? "Creating..." : "Create Workspace"}
          </button>
        {/if}
        {#if wsError}
          <span class="action-error">{wsError}</span>
        {/if}
      </div>

      {#if !hasWorktreeLinks && importAction}
        <div class="actions-row">
          <ActionButton
            class="btn--embedding-action"
            onclick={() => importAction.handler({
              surface: "pull-detail", owner, name, number,
            })}
            tone="neutral"
            surface="outline"
            size="sm"
          >
            {importAction.label}
          </ActionButton>
        </div>
      {/if}
      {#if hasWorktreeLinks && navigateAction}
        <div class="actions-row">
          {#each worktreeLinks as link (link.worktree_key)}
            <ActionButton
              class="btn--embedding-action"
              onclick={() => navigateAction.handler({
                surface: "pull-detail", owner, name, number,
                meta: { worktree_key: link.worktree_key },
              })}
              tone="neutral"
              surface="outline"
              size="sm"
            >
              {navigateAction.label}: {link.worktree_key}
            </ActionButton>
          {/each}
        </div>
      {/if}
      {#if otherActions.length > 0}
        <div class="actions-row">
          {#each otherActions as action (action.id)}
            <ActionButton
              class="btn--embedding-action"
              onclick={() => action.handler({
                surface: "pull-detail", owner, name, number,
              })}
              tone="neutral"
              surface="outline"
              size="sm"
            >
              {action.label}
            </ActionButton>
          {/each}
        </div>
      {/if}

      {#if showMergeModal && repoSettings}
        {@const d = detailStore.getDetail()!}
        {@const p = d.merge_request}
        <MergeModal
          {owner}
          {name}
          {number}
          prTitle={p.Title}
          prBody={p.Body}
          prAuthor={p.Author}
          prAuthorDisplayName={p.AuthorDisplayName}
          allowSquash={repoSettings.allowSquash}
          allowMerge={repoSettings.allowMerge}
          allowRebase={repoSettings.allowRebase}
          onclose={() => { showMergeModal = false; }}
          onmerged={() => {
            showMergeModal = false;
            void detailStore.loadDetail(owner, name, number);
            void pulls.loadPulls();
            void activity.loadActivity();
          }}
        />
      {/if}

      <!-- PR body -->
      {#if pr.Body}
        <div class="section body-section">
          <div class="section-header">
            <span class="section-title-inline">Description</span>
          </div>
          <div class="inset-box-wrap">
            <button
              class="copy-icon-btn"
              class:copied
              onclick={() => copyBody(pr.Body)}
              title={copied ? "Copied!" : "Copy to clipboard"}
            >
              {#if copied}
                <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
                  <path d="M13.78 4.22a.75.75 0 010 1.06l-7.25 7.25a.75.75 0 01-1.06 0L2.22 9.28a.75.75 0 011.06-1.06L6 10.94l6.72-6.72a.75.75 0 011.06 0z"/>
                </svg>
              {:else}
                <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
                  <path d="M0 6.75C0 5.784.784 5 1.75 5h1.5a.75.75 0 010 1.5h-1.5a.25.25 0 00-.25.25v7.5c0 .138.112.25.25.25h7.5a.25.25 0 00.25-.25v-1.5a.75.75 0 011.5 0v1.5A1.75 1.75 0 019.25 16h-7.5A1.75 1.75 0 010 14.25v-7.5z"/>
                  <path d="M5 1.75C5 .784 5.784 0 6.75 0h7.5C15.216 0 16 .784 16 1.75v7.5A1.75 1.75 0 0114.25 11h-7.5A1.75 1.75 0 015 9.25v-7.5zm1.75-.25a.25.25 0 00-.25.25v7.5c0 .138.112.25.25.25h7.5a.25.25 0 00.25-.25v-7.5a.25.25 0 00-.25-.25h-7.5z"/>
                </svg>
              {/if}
            </button>
            <div class="inset-box markdown-body">{@html renderMarkdown(pr.Body, { owner, name })}</div>
          </div>
        </div>
      {/if}

      <!-- Comment box -->
      <div class="section">
        <CommentBox {owner} {name} {number} />
      </div>

      <!-- Activity -->
      <div class="section">
        <h3 class="section-title">Activity</h3>
        {#if detailStore.getDetailLoaded()}
          <EventTimeline events={detail.events ?? []} repoOwner={owner} repoName={name} />
        {:else if detailStore.isDetailSyncing()}
          <div class="loading-placeholder">
            <svg class="sync-spinner" width="14" height="14" viewBox="0 0 16 16" fill="none">
              <circle cx="8" cy="8" r="6" stroke="currentColor" stroke-width="2" stroke-dasharray="28" stroke-dashoffset="8" stroke-linecap="round"/>
            </svg>
            Loading discussion...
          </div>
        {:else}
          <div class="loading-placeholder">Detail not yet loaded</div>
        {/if}
      </div>
        </div>
      {/if}
    </div>
  {/if}
{/if}

<style>
  .state-center {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
  }

  .state-msg {
    font-size: 13px;
    color: var(--text-muted);
  }

  .state-msg--error {
    color: var(--accent-red);
  }

  .pull-detail-wrap {
    display: flex;
    flex-direction: column;
    flex: 1;
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
    width: 280px;
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

  /* On narrow viewports the fixed 280px sidebar would crush the
     diff pane. Stack the sidebar above the diff with a capped
     height so the diff stays readable. */
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

    .files-main {
      flex: 1;
      min-height: 0;
    }
  }

  .pull-detail {
    padding: 20px 24px;
    max-width: 800px;
    display: flex;
    flex-direction: column;
    gap: 16px;
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    width: 100%;
    margin-inline: auto;
  }

  .detail-header {
    display: flex;
    align-items: flex-start;
    gap: 10px;
  }

  .detail-title {
    font-size: 18px;
    font-weight: 600;
    color: var(--text-primary);
    line-height: 1.35;
    flex: 1;
    min-width: 0;
  }

  .gh-link {
    flex-shrink: 0;
    color: var(--text-muted);
    display: flex;
    align-items: center;
    margin-top: 3px;
    transition: color 0.1s;
  }

  .gh-link:hover {
    color: var(--accent-blue);
    text-decoration: none;
  }

  .star-btn {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    margin-top: 3px;
    cursor: pointer;
    background: none;
    border: none;
    padding: 0;
  }

  .star-detail-icon {
    color: var(--text-muted);
    transition: color 0.1s;
  }

  .star-detail-icon:hover {
    color: var(--accent-amber);
  }

  .star-detail-icon--active {
    color: var(--accent-amber);
  }

  .meta-row {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 4px;
  }

  .meta-item {
    font-size: 12px;
    color: var(--text-secondary);
  }

  .meta-sep {
    font-size: 12px;
    color: var(--text-muted);
  }

  .sync-indicator {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 11px;
    color: var(--accent-blue);
  }

  .sync-spinner {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  .chips-row {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .chip {
    font-size: 11px;
    font-weight: 600;
    padding: 3px 8px;
    border-radius: 10px;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    white-space: nowrap;
  }

  .chip--green {
    background: color-mix(in srgb, var(--accent-green) 15%, transparent);
    color: var(--accent-green);
  }

  .chip--red {
    background: color-mix(in srgb, var(--accent-red) 15%, transparent);
    color: var(--accent-red);
  }

  .chip--amber {
    background: color-mix(in srgb, var(--accent-amber) 15%, transparent);
    color: var(--accent-amber);
  }

  .chip--purple {
    background: color-mix(in srgb, var(--accent-purple) 15%, transparent);
    color: var(--accent-purple);
  }

  .chip--muted {
    background: var(--bg-inset);
    color: var(--text-muted);
  }

  .chip--teal {
    background: color-mix(
      in srgb,
      var(--accent-teal, var(--accent-green)) 15%,
      transparent
    );
    color: var(--accent-teal, var(--accent-green));
  }

  .kanban-row {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .kanban-label {
    font-size: 12px;
    font-weight: 500;
    color: var(--text-secondary);
    flex-shrink: 0;
  }

  .kanban-select {
    font-size: 12px;
    font-weight: 600;
    padding: 4px 10px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-default);
    background: var(--bg-surface);
    cursor: pointer;
    outline: none;
  }

  .kanban-select:focus {
    border-color: var(--accent-blue);
  }

  .actions-row {
    display: flex;
    align-items: flex-start;
    gap: 8px;
  }

  .btn--workspace {
    padding: 4px 12px;
    border-radius: 6px;
    font-size: 12px;
    font-weight: 500;
    border: 1px solid var(--accent-blue, #0969da);
    background: var(--accent-blue, #0969da);
    color: #fff;
    cursor: pointer;
    transition: filter 0.1s;
  }
  .btn--workspace:hover {
    filter: brightness(1.1);
  }
  .btn--workspace:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .action-error {
    font-size: 11px;
    color: var(--accent-red, #d73a49);
  }

  .kanban-select--new { color: var(--kanban-new); }
  .kanban-select--reviewing { color: var(--accent-amber); }
  .kanban-select--waiting { color: var(--accent-purple); }
  .kanban-select--awaiting-merge { color: var(--accent-green); }

  .section {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .section-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .section-title {
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-muted);
  }

  .section-title-inline {
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-muted);
  }

  .inset-box-wrap {
    position: relative;
  }

  .copy-icon-btn {
    position: absolute;
    top: 6px;
    right: 6px;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    opacity: 0;
    transition: opacity 0.15s, background 0.15s, color 0.15s;
    z-index: 1;
  }

  .inset-box-wrap:hover .copy-icon-btn,
  .copy-icon-btn:focus-visible {
    opacity: 1;
  }

  .copy-icon-btn:hover {
    background: var(--bg-surface-hover);
    color: var(--text-secondary);
  }

  .copy-icon-btn:active {
    transform: scale(0.92);
  }

  .copy-icon-btn.copied {
    opacity: 1;
    color: var(--accent-green);
    background: color-mix(in srgb, var(--accent-green) 12%, transparent);
  }

  @media (hover: none) {
    .copy-icon-btn {
      opacity: 1;
    }
  }

  .inset-box {
    font-size: 13px;
    color: var(--text-primary);
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-sm);
    padding: 10px 12px;
    word-break: break-word;
    line-height: 1.6;
  }

  .merge-warning {
    font-size: 12px;
    padding: 8px 12px;
    border-radius: var(--radius-sm);
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .merge-warning a {
    color: inherit;
    text-decoration: underline;
    white-space: nowrap;
    flex-shrink: 0;
  }

  .merge-warning--conflict {
    background: color-mix(in srgb, var(--accent-amber) 12%, transparent);
    color: var(--accent-amber);
  }

  .merge-warning--info {
    background: color-mix(in srgb, var(--accent-blue) 10%, transparent);
    color: var(--text-secondary);
  }

  .files-stat {
    font-family: var(--font-mono);
    font-size: 12px;
    font-weight: 600;
  }

  .files-stat--add {
    color: var(--accent-green);
  }

  .files-stat--del {
    color: var(--accent-red);
  }

  .refresh-banner {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 4px 12px;
    background: var(--bg-inset);
    border-radius: var(--radius-sm);
    font-size: 11px;
    color: var(--text-secondary);
    margin-bottom: 8px;
  }

  .sync-dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--accent-green);
    animation: pulse 1.5s ease-in-out infinite;
  }

  @keyframes pulse {
    0%, 100% { opacity: 0.4; }
    50% { opacity: 1; }
  }

  .loading-placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 24px 0;
    font-size: 12px;
    color: var(--text-muted);
  }

  .detail-tabs {
    display: flex;
    gap: 0;
    border-bottom: 1px solid var(--border-default);
    background: var(--bg-surface);
    flex-shrink: 0;
  }

  .detail-tab {
    font-size: 12px;
    font-weight: 500;
    padding: 8px 16px;
    color: var(--text-secondary);
    border-bottom: 2px solid transparent;
    transition: color 0.1s, border-color 0.1s;
    display: flex;
    align-items: center;
    gap: 6px;
    background: none;
    border-top: none;
    border-left: none;
    border-right: none;
    cursor: pointer;
    font-family: inherit;
  }

  .detail-tab:hover {
    color: var(--text-primary);
    background: var(--bg-surface-hover);
  }

  .detail-tab--active {
    color: var(--text-primary);
    border-bottom-color: var(--accent-blue);
  }
</style>
