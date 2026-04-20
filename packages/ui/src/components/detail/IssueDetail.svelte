<script lang="ts">
  import {
    getStores, getClient, getActions,
    getUIConfig, getWorkspaceCommand,
  } from "../../context.js";
  import { renderMarkdown } from "../../utils/markdown.js";
  import { timeAgo } from "../../utils/time.js";
  import { copyToClipboard } from "../../utils/clipboard.js";
  import EventTimeline from "./EventTimeline.svelte";
  import IssueCommentBox from "./IssueCommentBox.svelte";
  import ActionButton from "../shared/ActionButton.svelte";
  import Chip from "../shared/Chip.svelte";
  import GitHubLabels from "../shared/GitHubLabels.svelte";

  const { issues, activity } = getStores();
  const client = getClient();
  const actions = getActions();
  const uiConfig = getUIConfig();
  const workspaceCommand = getWorkspaceCommand();

  interface Props {
    owner: string;
    name: string;
    number: number;
  }

  const { owner, name, number }: Props = $props();

  $effect(() => {
    void issues.loadIssueDetail(owner, name, number);
    issues.startIssueDetailPolling(owner, name, number);
    return () => issues.stopIssueDetailPolling();
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

  function handleStarClick(): void {
    const detail = issues.getIssueDetail();
    if (!detail) return;
    void issues.toggleIssueStar(
      owner,
      name,
      number,
      detail.issue.Starred,
    );
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
        "/repos/{owner}/{name}/issues/{number}/github-state",
        {
          params: { path: { owner, name, number } },
          body: { state: newState },
        },
      );
      if (requestError) {
        throw new Error(
          requestError.detail
            ?? requestError.title
            ?? "failed to change issue state",
        );
      }
      await issues.loadIssueDetail(owner, name, number);
      await issues.loadIssues();
      await activity.loadActivity();
    } catch (err) {
      stateError =
        err instanceof Error ? err.message : String(err);
    } finally {
      stateSubmitting = false;
    }
  }

  let workspaceCreating = $state(false);
  let workspaceError = $state<string | null>(null);

  async function createWorkspace(): Promise<void> {
    const detail = issues.getIssueDetail();
    if (!detail || workspaceCommand === null) return;

    workspaceCreating = true;
    workspaceError = null;
    try {
      const result = await workspaceCommand(
        "createWorktreeFromIssue",
        {
          platformHost: detail.platform_host,
          owner: detail.repo_owner,
          name: detail.repo_name,
          number: detail.issue.Number,
        },
      );
      if (!result.ok) {
        throw new Error(
          result.message ?? "failed to create workspace",
        );
      }
    } catch (err) {
      workspaceError =
        err instanceof Error ? err.message : String(err);
    } finally {
      workspaceCreating = false;
    }
  }
</script>

{#if issues.isIssueDetailLoading()}
  <div class="state-center"><p class="state-msg">Loading...</p></div>
{:else if issues.getIssueDetailError() !== null && issues.getIssueDetail() === null}
  <div class="state-center"><p class="state-msg state-msg--error">Error: {issues.getIssueDetailError()}</p></div>
{:else}
  {@const detail = issues.getIssueDetail()}
  {#if detail !== null}
    {@const issue = detail.issue}
    {@const labels = issue.labels ?? []}
    <div class="issue-detail">
      {#if issues.isIssueStaleRefreshing()}
        <div class="refresh-banner">
          <span class="sync-dot"></span>
          Refreshing...
        </div>
      {/if}
      <!-- Header -->
      <div class="detail-header">
        <h2 class="detail-title">{issue.Title}</h2>
        {#if !uiConfig.hideStar}
          <button
            class="star-btn"
            onclick={handleStarClick}
            title={issue.Starred ? "Unstar" : "Star"}
          >
            {#if issue.Starred}
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
        <a class="gh-link" href={issue.URL} target="_blank" rel="noopener noreferrer" title="Open on GitHub">
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
        <span class="meta-item">#{issue.Number}</span>
        <span class="meta-sep">·</span>
        <span class="meta-item">{issue.Author}</span>
        <span class="meta-sep">·</span>
        <span class="meta-item">{timeAgo(issue.CreatedAt)}</span>
        <span class="meta-sep">·</span>
        <Chip size="sm" class={`issue-state-chip chip--${issue.State}`}>
          {issue.State === "open" ? "Open" : "Closed"}
        </Chip>
        {#if issues.isIssueDetailSyncing()}
          <span class="meta-sep">·</span>
          <span class="sync-indicator" title="Syncing from GitHub">
            <svg class="sync-spinner" width="12" height="12" viewBox="0 0 16 16" fill="none">
              <circle cx="8" cy="8" r="6" stroke="currentColor" stroke-width="2" stroke-dasharray="28" stroke-dashoffset="8" stroke-linecap="round"/>
            </svg>
            Syncing
          </span>
        {/if}
      </div>

      <!-- Labels -->
      {#if labels.length > 0}
        <GitHubLabels {labels} mode="full" />
      {/if}

      <!-- Issue body -->
      {#if issue.Body}
        <div class="section body-section">
          <div class="section-header">
            <span class="section-title-inline">Description</span>
          </div>
          <div class="inset-box-wrap">
            <button
              class="copy-icon-btn"
              class:copied
              onclick={() => copyBody(issue.Body)}
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
            <div class="inset-box markdown-body">{@html renderMarkdown(issue.Body, { owner, name })}</div>
          </div>
        </div>
      {/if}

      <!-- Actions -->
      <div class="actions-row">
        {#if workspaceCommand !== null}
          <button
            class="btn--workspace"
            disabled={workspaceCreating}
            onclick={() => void createWorkspace()}
          >
            {workspaceCreating ? "Creating..." : "Create Workspace"}
          </button>
        {/if}
        {#if issue.State === "open"}
          <ActionButton
            class="btn--close"
            disabled={stateSubmitting}
            onclick={() => handleStateChange("closed")}
            tone="danger"
            surface="outline"
            size="sm"
          >
            {stateSubmitting ? "Closing..." : "Close issue"}
          </ActionButton>
        {:else}
          <ActionButton
            class="btn--reopen"
            disabled={stateSubmitting}
            onclick={() => handleStateChange("open")}
            tone="success"
            surface="solid"
            size="sm"
          >
            {stateSubmitting ? "Reopening..." : "Reopen issue"}
          </ActionButton>
        {/if}
        {#if workspaceError}
          <span class="action-error">{workspaceError}</span>
        {/if}
        {#each actions.issue ?? [] as action (action.id)}
          <ActionButton
            class="btn--embedding-action"
            onclick={() => action.handler({ surface: "issue-detail", owner, name, number })}
            tone="neutral"
            surface="outline"
            size="sm"
          >
            {action.label}
          </ActionButton>
        {/each}
        {#if stateError}
          <span class="action-error">{stateError}</span>
        {/if}
      </div>

      <!-- Comment box -->
      <div class="section">
        <IssueCommentBox {owner} {name} {number} />
      </div>

      <!-- Activity -->
      <div class="section">
        <h3 class="section-title">Activity</h3>
        {#if issues.getIssueDetailLoaded()}
          <EventTimeline events={detail.events ?? []} repoOwner={owner} repoName={name} />
        {:else if issues.isIssueDetailSyncing()}
          <div class="loading-placeholder">
            <svg class="sync-spinner" width="14" height="14" viewBox="0 0 16 16" fill="none">
              <circle cx="8" cy="8" r="6" stroke="currentColor" stroke-width="2" stroke-dasharray="28" stroke-dashoffset="8" stroke-linecap="round"/>
            </svg>
            Loading comments...
          </div>
        {:else}
          <div class="loading-placeholder">Detail not yet loaded</div>
        {/if}
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

  .issue-detail {
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

  .actions-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 0;
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

</style>
