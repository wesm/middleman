<script lang="ts">
  import { untrack } from "svelte";
  import {
    getStores, getClient, getActions,
    getUIConfig, getNavigate,
  } from "../../context.js";
  import type { IssueDetailSyncMode } from "../../stores/issues.svelte.js";
  import { renderMarkdown } from "../../utils/markdown.js";
  import { timeAgo } from "../../utils/time.js";
  import { copyToClipboard } from "../../utils/clipboard.js";
  import EventTimeline from "./EventTimeline.svelte";
  import IssueCommentBox from "./IssueCommentBox.svelte";
  import ActionButton from "../shared/ActionButton.svelte";
  import Chip from "../shared/Chip.svelte";
  import GitHubLabels from "../shared/GitHubLabels.svelte";
  import CopyItemNumber from "./CopyItemNumber.svelte";
  import MonitorUpIcon from "@lucide/svelte/icons/monitor-up";
  import PackagePlusIcon from "@lucide/svelte/icons/package-plus";
  import RefreshCwIcon from "@lucide/svelte/icons/refresh-cw";
  import XIcon from "@lucide/svelte/icons/x";

  const { issues, activity } = getStores();
  const client = getClient();
  const actions = getActions();
  const uiConfig = getUIConfig();
  const navigate = getNavigate();

  interface Props {
    owner: string;
    name: string;
    number: number;
    platformHost?: string | undefined;
    autoSync?: IssueDetailSyncMode;
  }

  const {
    owner,
    name,
    number,
    platformHost,
    autoSync = "background",
  }: Props = $props();

  // See PullDetail.svelte: while a route change is in flight, the
  // displayed issue may briefly belong to the previous route. Mutating
  // actions (state change, workspace create, etc.) read the props,
  // which point at the new route — so they must be gated until the
  // displayed issue catches up.
  const staleIssue = $derived.by(() => {
    const d = issues.getIssueDetail();
    if (d == null) return false;
    if (
      d.repo_owner !== owner ||
      d.repo_name !== name ||
      (d.issue?.Number ?? -1) !== number
    ) {
      return true;
    }
    // The API treats an absent platform_host as github.com, so the
    // comparison must normalize both sides to the same default.
    // Otherwise, navigating from a non-default host to the same
    // owner/repo/number without a platformHost would render the
    // previous host's detail as current.
    const expectedHost = platformHost ?? "github.com";
    const actualHost = d.platform_host || "github.com";
    return actualHost !== expectedHost;
  });

  $effect(() => {
    const requestOwner = owner;
    const requestName = name;
    const requestNumber = number;
    const requestPlatformHost = platformHost;
    const requestAutoSync = autoSync;
    untrack(() => {
      void issues.loadIssueDetail(
        requestOwner,
        requestName,
        requestNumber,
        requestPlatformHost,
        { sync: requestAutoSync },
      );
      issues.startIssueDetailPolling(
        requestOwner,
        requestName,
        requestNumber,
        requestPlatformHost,
      );
    });
    return () => issues.stopIssueDetailPolling();
  });

  // Clear conflict/error state on route change so issue A's
  // dialogs can't bleed into issue B's view.
  $effect(() => {
    void owner;
    void name;
    void number;
    branchConflict = null;
    workspaceCreating = false;
    workspaceError = null;
    stateError = null;
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
    if (staleIssue) return;
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
    if (staleIssue) return;
    stateSubmitting = true;
    stateError = null;
    try {
      const detail = issues.getIssueDetail();
      const { error: requestError } = await client.POST(
        "/repos/{owner}/{name}/issues/{number}/github-state",
        {
          params: { path: { owner, name, number } },
          body: {
            state: newState,
            ...(detail && {
              platform_host: detail.platform_host,
            }),
          },
        },
      );
      if (requestError) {
        throw new Error(
          requestError.detail
            ?? requestError.title
            ?? "failed to change issue state",
        );
      }
      await issues.loadIssueDetail(
        owner,
        name,
        number,
        detail?.platform_host ?? platformHost,
      );
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
  const ISSUE_WORKSPACE_BRANCH_CONFLICT_TYPE =
    "urn:middleman:error:issue-workspace-branch-conflict";

  type APIErrorDetail = {
    location?: string;
    value?: unknown;
  };

  type APIError = {
    type?: string;
    title?: string;
    detail?: string;
    errors?: APIErrorDetail[] | null;
  };

  type BranchConflictState = {
    existingBranch: string;
    suggestedBranch: string;
    branchInput: string;
    error: string | null;
  };

  let branchConflict = $state<BranchConflictState | null>(
    null,
  );
  const workspace = $derived(
    issues.getIssueDetail()?.workspace,
  );

  function issueWorkspaceBranch(): string {
    return `middleman/issue-${number}`;
  }

  function branchConflictValue(
    error: APIError,
    location: string,
  ): string | null {
    const value = error.errors?.find(
      (entry) => entry.location === location,
    )?.value;
    return typeof value === "string" && value
      ? value
      : null;
  }

  function parseBranchConflict(
    error: APIError | undefined,
  ): BranchConflictState | null {
    if (!error) {
      return null;
    }

    const existingBranch =
      branchConflictValue(error, "body.git_head_ref")
      ?? "";
    const suggestedBranch =
      branchConflictValue(
        error,
        "body.suggested_git_head_ref",
      )
      ?? "";
    const isTypedConflict =
      error.type === ISSUE_WORKSPACE_BRANCH_CONFLICT_TYPE;
    if (
      !isTypedConflict
      && (!existingBranch || !suggestedBranch)
    ) {
      return null;
    }

    return {
      existingBranch:
        existingBranch || issueWorkspaceBranch(),
      suggestedBranch:
        suggestedBranch
        || `${existingBranch || issueWorkspaceBranch()}-2`,
      branchInput:
        suggestedBranch
        || `${existingBranch || issueWorkspaceBranch()}-2`,
      error: null,
    };
  }

  type CreateWorkspaceOptions = {
    gitHeadRef?: string;
    reuseExistingBranch?: boolean;
    fromConflictDialog?: boolean;
  };

  async function createWorkspace(
    options: CreateWorkspaceOptions = {},
  ): Promise<void> {
    if (staleIssue) return;
    const detail = issues.getIssueDetail();
    if (!detail) return;

    if (!options.fromConflictDialog) {
      branchConflict = null;
    } else if (
      branchConflict
      && options.gitHeadRef?.trim() === ""
    ) {
      branchConflict.error =
        "Branch name cannot be empty.";
      return;
    }

    workspaceCreating = true;
    workspaceError = null;
    if (branchConflict) {
      branchConflict.error = null;
    }
    try {
      const { data, error: requestError } = await client.POST(
        "/repos/{owner}/{name}/issues/{number}/workspace",
        {
          params: {
            path: {
              owner,
              name,
              number,
            },
          },
          body: {
            platform_host: detail.platform_host,
            ...(options.gitHeadRef
              ? {
                  git_head_ref:
                    options.gitHeadRef.trim(),
                }
              : {}),
            ...(options.reuseExistingBranch
              ? {
                  reuse_existing_branch: true,
                }
              : {}),
          },
        },
      );
      if (requestError) {
        const conflict = parseBranchConflict(
          requestError as APIError,
        );
        if (conflict) {
          branchConflict = conflict;
          return;
        }

        const message =
          requestError.detail
          ?? requestError.title
          ?? "failed to create workspace";
        if (options.fromConflictDialog && branchConflict) {
          branchConflict.error = message;
          return;
        }
        throw new Error(
          message,
        );
      }
      if (data?.id) {
        navigate(`/terminal/${data.id}`);
      }
    } catch (err) {
      workspaceError =
        err instanceof Error ? err.message : String(err);
    } finally {
      workspaceCreating = false;
    }
  }

  function closeBranchConflictDialog(): void {
    if (workspaceCreating) return;
    branchConflict = null;
  }
</script>

{#if issues.isIssueDetailLoading() && (issues.getIssueDetail() === null || staleIssue)}
  <div class="state-center"><p class="state-msg">Loading...</p></div>
{:else if issues.getIssueDetailError() !== null && (issues.getIssueDetail() === null || staleIssue)}
  <div class="state-center"><p class="state-msg state-msg--error">Error: {issues.getIssueDetailError()}</p></div>
{:else}
  {@const detail = issues.getIssueDetail()}
  {#if detail !== null && !staleIssue}
    {@const issue = detail.issue}
    {@const labels = issue.labels ?? []}
    <div class="issue-detail">
      {#if staleIssue && issues.getIssueDetailError() !== null}
        <div class="detail-load-error" data-testid="detail-load-error">
          Couldn't load this issue: {issues.getIssueDetailError()}
        </div>
      {/if}
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
            disabled={staleIssue}
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
        <CopyItemNumber kind="issue" number={issue.Number} url={issue.URL} />
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
        {#if workspace}
          <ActionButton
            class="btn--workspace"
            onclick={() => navigate(`/terminal/${workspace.id}`)}
            tone="info"
            surface="soft"
            size="sm"
            label="Open Workspace"
            shortLabel="Workspace"
          >
            <MonitorUpIcon size="14" strokeWidth="2.2" aria-hidden="true" />
          </ActionButton>
        {:else}
          <ActionButton
            class="btn--workspace"
            disabled={workspaceCreating || staleIssue}
            onclick={() => void createWorkspace()}
            tone="info"
            surface="soft"
            size="sm"
            label={workspaceCreating ? "Creating..." : "Create Workspace"}
            shortLabel={workspaceCreating ? "Creating..." : "Create Workspace"}
          >
            <PackagePlusIcon size="14" strokeWidth="2.2" aria-hidden="true" />
          </ActionButton>
        {/if}
        {#if issue.State === "open"}
          <ActionButton
            class="btn--close"
            disabled={stateSubmitting || staleIssue}
            onclick={() => handleStateChange("closed")}
            tone="danger"
            surface="outline"
            size="sm"
            label={stateSubmitting ? "Closing..." : "Close issue"}
            shortLabel={stateSubmitting ? "Closing..." : "Close"}
          >
            <XIcon size="14" strokeWidth="2.2" aria-hidden="true" />
          </ActionButton>
        {:else}
          <ActionButton
            class="btn--reopen"
            disabled={stateSubmitting || staleIssue}
            onclick={() => handleStateChange("open")}
            tone="success"
            surface="solid"
            size="sm"
            label={stateSubmitting ? "Reopening..." : "Reopen issue"}
            shortLabel={stateSubmitting ? "Reopening..." : "Reopen"}
          >
            <RefreshCwIcon size="14" strokeWidth="2.2" aria-hidden="true" />
          </ActionButton>
        {/if}
        {#if workspaceError}
          <span class="action-error">{workspaceError}</span>
        {/if}
        {#each actions.issue ?? [] as action (action.id)}
          <ActionButton
            class="btn--embedding-action"
            onclick={() => {
              if (staleIssue) return;
              action.handler({
                surface: "issue-detail", owner, name, number,
              });
            }}
            disabled={staleIssue}
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
        <IssueCommentBox
          {owner}
          {name}
          {number}
          platformHost={detail.platform_host}
          disabled={staleIssue}
        />
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

    {#if branchConflict}
      {@const conflict = branchConflict}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="modal-overlay"
        onclick={closeBranchConflictDialog}
        onkeydown={(event) => {
          if (event.key === "Escape") {
            event.preventDefault();
            closeBranchConflictDialog();
          }
        }}
      >
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="modal"
          role="dialog"
          aria-modal="true"
          aria-labelledby="issue-workspace-branch-conflict-title"
          tabindex="-1"
          onclick={(event) => event.stopPropagation()}
        >
          <div class="modal-header">
            <h3
              id="issue-workspace-branch-conflict-title"
              class="modal-title"
            >
              Branch Name Conflict
            </h3>
            <button
              class="modal-close"
              onclick={closeBranchConflictDialog}
              title="Cancel (Esc)"
              disabled={workspaceCreating}
            >
              <svg
                width="16"
                height="16"
                viewBox="0 0 16 16"
                fill="currentColor"
              >
                <path d="M3.72 3.72a.75.75 0 011.06 0L8 6.94l3.22-3.22a.75.75 0 111.06 1.06L9.06 8l3.22 3.22a.75.75 0 11-1.06 1.06L8 9.06l-3.22 3.22a.75.75 0 01-1.06-1.06L6.94 8 3.72 4.78a.75.75 0 010-1.06z"/>
              </svg>
            </button>
          </div>

          <div class="modal-body">
            <p class="modal-copy">
              The branch <code>{conflict.existingBranch}</code> already exists locally.
            </p>

            <div class="branch-conflict-option">
              <div>
                <div class="branch-conflict-heading">
                  Reuse the existing branch
                </div>
                <div class="branch-conflict-copy">
                  Reopen the workspace on the branch that is already present in the local clone.
                </div>
              </div>
              <ActionButton
                class="btn btn--primary"
                onclick={() => void createWorkspace({
                  gitHeadRef: conflict.existingBranch,
                  reuseExistingBranch: true,
                  fromConflictDialog: true,
                })}
                disabled={workspaceCreating}
                tone="neutral"
                surface="outline"
                size="sm"
              >
                {workspaceCreating ? "Creating..." : "Use Existing Branch"}
              </ActionButton>
            </div>

            <div class="field">
              <label
                class="field-label"
                for="issue-workspace-branch-name"
              >
                New branch name
              </label>
              <input
                id="issue-workspace-branch-name"
                class="field-input"
                type="text"
                bind:value={conflict.branchInput}
                oninput={() => {
                  if (branchConflict) {
                    branchConflict.error = null;
                  }
                }}
              />
              <p class="field-hint">
                Suggested: <code>{conflict.suggestedBranch}</code>
              </p>
            </div>

            {#if conflict.error}
              <p class="merge-error">{conflict.error}</p>
            {/if}
          </div>

          <div class="modal-footer">
            <ActionButton
              class="btn btn--secondary"
              onclick={closeBranchConflictDialog}
              disabled={workspaceCreating}
              tone="neutral"
              surface="outline"
            >
              Cancel
            </ActionButton>
            <ActionButton
              class="btn btn--primary btn--green"
              onclick={() => void createWorkspace({
                gitHeadRef: conflict.branchInput,
                fromConflictDialog: true,
              })}
              disabled={workspaceCreating}
              tone="success"
              surface="solid"
            >
              {workspaceCreating ? "Creating..." : "Create New Branch"}
            </ActionButton>
          </div>
        </div>
      </div>
    {/if}
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

  .detail-load-error {
    padding: 6px 16px;
    background: var(--accent-red-soft, color-mix(in srgb, var(--accent-red) 12%, transparent));
    color: var(--accent-red);
    border-bottom: 1px solid var(--border-subtle);
    font-size: 12px;
    flex-shrink: 0;
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

  .modal-overlay {
    position: fixed;
    inset: 0;
    background: var(--overlay-bg);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 50;
    animation: fade-in 0.12s ease-out;
  }

  @keyframes fade-in {
    from { opacity: 0; }
    to { opacity: 1; }
  }

  .modal {
    width: min(560px, 92vw);
    background: var(--bg-surface);
    border: 1px solid var(--border-muted);
    border-radius: 12px;
    box-shadow: var(--shadow-lg);
    overflow: hidden;
  }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 16px;
    border-bottom: 1px solid var(--border-muted);
  }

  .modal-title {
    margin: 0;
    font-size: 15px;
    font-weight: 600;
    color: var(--text-primary);
  }

  .modal-close {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    border: 1px solid transparent;
    border-radius: 8px;
    background: transparent;
    color: var(--text-secondary);
    cursor: pointer;
  }

  .modal-close:hover:not(:disabled) {
    background: var(--bg-inset);
    color: var(--text-primary);
  }

  .modal-close:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .modal-body {
    padding: 16px;
    display: grid;
    gap: 14px;
  }

  .modal-copy {
    margin: 0;
    font-size: 13px;
    color: var(--text-secondary);
    line-height: 1.5;
  }

  .branch-conflict-option {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    padding: 12px;
    border: 1px solid var(--border-muted);
    border-radius: 10px;
    background: var(--bg-inset);
  }

  .branch-conflict-heading {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
    margin-bottom: 4px;
  }

  .branch-conflict-copy {
    font-size: 12px;
    color: var(--text-secondary);
    line-height: 1.5;
  }

  .field {
    display: grid;
    gap: 6px;
  }

  .field-label {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-primary);
  }

  .field-input {
    width: 100%;
    min-width: 0;
    padding: 9px 11px;
    border: 1px solid var(--border-muted);
    border-radius: 8px;
    background: var(--bg-canvas);
    color: var(--text-primary);
    font-size: 13px;
  }

  .field-hint {
    margin: 0;
    font-size: 11px;
    color: var(--text-muted);
  }

  .merge-error {
    margin: 0;
    font-size: 12px;
    color: var(--accent-red, #d73a49);
  }

  .modal-footer {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding: 14px 16px;
    border-top: 1px solid var(--border-muted);
  }

</style>
