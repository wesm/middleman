<script lang="ts">
  import {
    getDetail,
    isDetailLoading,
    isDetailSyncing,
    getDetailError,
    loadDetail,
    updateKanbanState,
    startDetailPolling,
    stopDetailPolling,
    toggleDetailPRStar,
  } from "../../stores/detail.svelte.js";
  import { loadPulls } from "../../stores/pulls.svelte.js";
  import { loadActivity } from "../../stores/activity.svelte.js";
  import { client } from "../../api/runtime.js";
  import type { CICheck, KanbanStatus } from "../../api/types.js";
  import { navigate } from "../../stores/router.svelte.js";
  import { renderMarkdown } from "../../utils/markdown.js";
  import { timeAgo } from "../../utils/time.js";
  import { copyToClipboard } from "../../utils/clipboard.js";
  import EventTimeline from "./EventTimeline.svelte";
  import CommentBox from "./CommentBox.svelte";
  import ApproveButton from "./ApproveButton.svelte";
  import MergeModal from "./MergeModal.svelte";
  import ReadyForReviewButton from "./ReadyForReviewButton.svelte";
  import { getPullRequestActions, invokeAction, getUIConfig } from "../../stores/embed-config.svelte.js";

  interface Props {
    owner: string;
    name: string;
    number: number;
    onPullsRefresh?: () => Promise<void>;
  }

  const { owner, name, number, onPullsRefresh }: Props = $props();

  $effect(() => {
    void loadDetail(owner, name, number);
    startDetailPolling(owner, name, number);
    return () => stopDetailPolling();
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
      await loadPulls();
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
      await loadDetail(owner, name, number);
      await refreshPulls();
      await loadActivity();
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

  let ciExpanded = $state(false);
  const checks = $derived(parseCIChecks(getDetail()?.pull_request?.CIChecksJSON ?? ""));
  const failedChecks = $derived(checks.filter(c => c.conclusion === "failure"));

  function parseCIChecks(json: string): CICheck[] {
    if (!json) return [];
    try {
      return JSON.parse(json) as CICheck[];
    } catch {
      return [];
    }
  }

  function checkIcon(c: CICheck): string {
    if (c.status !== "completed") return "◦";
    if (c.conclusion === "success") return "✓";
    if (c.conclusion === "failure") return "✗";
    if (c.conclusion === "skipped" || c.conclusion === "neutral") return "–";
    return "?";
  }

  function checkColor(c: CICheck): string {
    if (c.status !== "completed") return "var(--accent-amber)";
    if (c.conclusion === "success") return "var(--accent-green)";
    if (c.conclusion === "failure") return "var(--accent-red)";
    return "var(--text-muted)";
  }

  const kanbanOptions: { value: KanbanStatus; label: string }[] = [
    { value: "new", label: "New" },
    { value: "reviewing", label: "Reviewing" },
    { value: "waiting", label: "Waiting" },
    { value: "awaiting_merge", label: "Awaiting Merge" },
  ];

  function ciColor(status: string): string {
    if (status === "success") return "chip--green";
    if (status === "failure" || status === "error") return "chip--red";
    if (status === "pending") return "chip--amber";
    return "chip--muted";
  }

  function reviewColor(decision: string): string {
    if (decision === "APPROVED") return "chip--green";
    if (decision === "CHANGES_REQUESTED") return "chip--red";
    return "chip--muted";
  }

  function onKanbanChange(e: Event): void {
    const select = e.target as HTMLSelectElement;
    void updateKanbanState(owner, name, number, select.value as KanbanStatus);
  }
</script>

{#if isDetailLoading()}
  <div class="state-center"><p class="state-msg">Loading…</p></div>
{:else if getDetailError() !== null && getDetail() === null}
  <div class="state-center"><p class="state-msg state-msg--error">Error: {getDetailError()}</p></div>
{:else}
  {@const detail = getDetail()}
  {#if detail !== null}
    {@const pr = detail.pull_request}
    <div class="pull-detail">
      <!-- Header -->
      <div class="detail-header">
        <h2 class="detail-title">{pr.Title}</h2>
        {#if !getUIConfig().hideStar}
          <button
            class="star-btn"
            onclick={() => void toggleDetailPRStar(owner, name, number, pr.Starred)}
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
        {#if isDetailSyncing()}
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
        {#if pr.CIStatus || checks.length > 0}
          <button
            class="chip chip--clickable {ciColor(pr.CIStatus)}"
            onclick={() => { ciExpanded = !ciExpanded; }}
            title={ciExpanded ? "Collapse CI checks" : "Expand CI checks"}
          >
            CI: {pr.CIStatus || "unknown"}
            {#if checks.length > 0}
              ({checks.length})
            {/if}
            <span class="chip-chevron" class:chip-chevron--open={ciExpanded}>▾</span>
          </button>
        {/if}
        {#if pr.ReviewDecision}
          <span class="chip {reviewColor(pr.ReviewDecision)}">{pr.ReviewDecision.replace(/_/g, " ")}</span>
        {/if}
        {#if pr.Additions > 0 || pr.Deletions > 0}
          <span class="chip chip--muted">+{pr.Additions}/-{pr.Deletions}</span>
        {/if}
      </div>

      <!-- Expanded CI checks -->
      {#if ciExpanded && checks.length > 0}
        <div class="ci-checks">
          {#if failedChecks.length > 0}
            <div class="ci-section-label ci-section-label--red">Failed ({failedChecks.length})</div>
            {#each failedChecks as check}
              <a
                class="ci-check"
                href={check.url}
                target="_blank"
                rel="noopener noreferrer"
              >
                <span class="ci-icon" style="color: {checkColor(check)}">{checkIcon(check)}</span>
                <span class="ci-name">{check.name}</span>
                {#if check.app}
                  <span class="ci-app">{check.app}</span>
                {/if}
                <span class="ci-arrow">→</span>
              </a>
            {/each}
          {/if}
          {#each checks.filter(c => c.conclusion !== "failure") as check}
            <a
              class="ci-check"
              href={check.url}
              target="_blank"
              rel="noopener noreferrer"
            >
              <span class="ci-icon" style="color: {checkColor(check)}">{checkIcon(check)}</span>
              <span class="ci-name">{check.name}</span>
              {#if check.app}
                <span class="ci-app">{check.app}</span>
              {/if}
              <span class="ci-arrow">→</span>
            </a>
          {/each}
        </div>
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

      <!-- Approve / Merge / Close / Reopen actions -->
      {#if pr.State !== "merged"}
        <div class="actions-row">
          {#if pr.State === "open"}
            {#if pr.IsDraft}
              <ReadyForReviewButton {owner} {name} {number} />
            {/if}
            <ApproveButton {owner} {name} {number} />
            {#if repoSettings}
              <button class="btn--merge" onclick={() => { showMergeModal = true; }}>
                {#if repoSettings.allowSquash && !repoSettings.allowMerge && !repoSettings.allowRebase}
                  Squash and merge
                {:else if !repoSettings.allowSquash && repoSettings.allowMerge && !repoSettings.allowRebase}
                  Merge
                {:else if !repoSettings.allowSquash && !repoSettings.allowMerge && repoSettings.allowRebase}
                  Rebase and merge
                {:else}
                  Merge &#9662;
                {/if}
              </button>
            {/if}
            <button class="btn--close" disabled={stateSubmitting} onclick={() => handleStateChange("closed")}>
              {stateSubmitting ? "Closing..." : "Close"}
            </button>
          {:else if pr.State === "closed"}
            <button class="btn--reopen" disabled={stateSubmitting} onclick={() => handleStateChange("open")}>
              {stateSubmitting ? "Reopening..." : "Reopen"}
            </button>
          {/if}
          {#if stateError}
            <span class="action-error">{stateError}</span>
          {/if}
        </div>
      {/if}

      {#if getPullRequestActions().length > 0}
        <div class="actions-row">
          {#each getPullRequestActions() as action (action.id)}
            <button
              class="btn--embedding-action"
              onclick={() => invokeAction(action, { owner, name, number })}
            >
              {action.label}
            </button>
          {/each}
        </div>
      {/if}

      {#if showMergeModal && repoSettings}
        {@const d = getDetail()!}
        {@const p = d.pull_request}
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
            void loadDetail(owner, name, number);
            void loadPulls();
            void loadActivity();
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

      <!-- Files changed -->
      <button
        class="files-changed-btn"
        onclick={() => navigate(`/pulls/${owner}/${name}/${number}/files`)}
      >
        <span class="files-changed-label">Files changed</span>
        <span class="files-changed-stats">
          {#if pr.Additions > 0}
            <span class="files-stat files-stat--add">+{pr.Additions}</span>
          {/if}
          {#if pr.Deletions > 0}
            <span class="files-stat files-stat--del">-{pr.Deletions}</span>
          {/if}
        </span>
        <span class="files-changed-arrow">&#8594;</span>
      </button>

      <!-- Comment box -->
      <div class="section">
        <CommentBox {owner} {name} {number} />
      </div>

      <!-- Activity -->
      <div class="section">
        <h3 class="section-title">Activity</h3>
        <EventTimeline events={detail.events ?? []} repoOwner={owner} repoName={name} />
      </div>
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

  .pull-detail {
    padding: 20px 24px;
    max-width: 800px;
    display: flex;
    flex-direction: column;
    gap: 16px;
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

  .chip--clickable {
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    transition: opacity 0.1s;
  }
  .chip--clickable:hover {
    opacity: 0.8;
  }
  .chip-chevron {
    font-size: 10px;
    transition: transform 0.15s;
  }
  .chip-chevron--open {
    transform: rotate(180deg);
  }

  .ci-checks {
    display: flex;
    flex-direction: column;
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-md);
    overflow: hidden;
  }
  .ci-section-label {
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    padding: 6px 12px 4px;
    color: var(--text-muted);
  }
  .ci-section-label--red {
    color: var(--accent-red);
  }
  .ci-check {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 12px;
    font-size: 12px;
    color: var(--text-primary);
    text-decoration: none;
    transition: background 0.08s;
  }
  .ci-check:hover {
    background: var(--bg-surface-hover);
    text-decoration: none;
  }
  .ci-check + .ci-check {
    border-top: 1px solid var(--border-muted);
  }
  .ci-icon {
    font-weight: 700;
    font-size: 13px;
    flex-shrink: 0;
    width: 16px;
    text-align: center;
  }
  .ci-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .ci-app {
    font-size: 10px;
    color: var(--text-muted);
    flex-shrink: 0;
  }
  .ci-arrow {
    color: var(--text-muted);
    flex-shrink: 0;
    font-size: 12px;
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

  .btn--merge {
    font-size: 13px;
    font-weight: 500;
    padding: 6px 14px;
    border-radius: var(--radius-sm);
    background: #1a7f37;
    color: #e6ffe6;
    border: none;
    cursor: pointer;
    transition: background 0.1s;
  }
  .btn--merge:hover {
    background: #176b2e;
  }

  .btn--close {
    padding: 4px 12px;
    border-radius: 6px;
    font-size: 12px;
    font-weight: 500;
    border: 1px solid var(--border-default);
    background: var(--bg-surface);
    color: var(--text-secondary);
    cursor: pointer;
  }
  .btn--close:hover {
    background: var(--accent-red, #d73a49);
    color: #fff;
    border-color: var(--accent-red, #d73a49);
  }
  .btn--reopen {
    padding: 4px 12px;
    border-radius: 6px;
    font-size: 12px;
    font-weight: 500;
    border: 1px solid var(--accent-green, #2ea043);
    background: var(--accent-green, #2ea043);
    color: #fff;
    cursor: pointer;
  }
  .btn--reopen:hover {
    filter: brightness(1.1);
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

  .files-changed-btn {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 10px 14px;
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-md);
    cursor: pointer;
    transition: background 0.1s, border-color 0.1s;
  }

  .files-changed-btn:hover {
    background: var(--bg-surface-hover);
    border-color: var(--accent-blue);
  }

  .files-changed-label {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
  }

  .files-changed-stats {
    display: flex;
    gap: 6px;
    flex: 1;
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

  .files-changed-arrow {
    font-size: 14px;
    color: var(--text-muted);
    transition: color 0.1s;
  }

  .files-changed-btn:hover .files-changed-arrow {
    color: var(--accent-blue);
  }

</style>
