<script lang="ts">
  import { ActionButton, Chip } from "@middleman/ui";
  import { timeAgo } from "@middleman/ui/utils/time";
  import RepoIssueComposer from "./RepoIssueComposer.svelte";
  import RepoMetricGrid from "./RepoMetricGrid.svelte";
  import {
    localDateTimeLabel,
    repoKey,
    type RepoMetric,
    type RepoSummaryCard,
  } from "./repoSummary.js";

  interface Props {
    summary: RepoSummaryCard;
    composerOpen: boolean;
    issueTitle: string;
    issueBody: string;
    issueError?: string | null;
    issueSubmitting?: boolean;
    onviewprs: () => void;
    onviewissues: () => void;
    onopencomposer: () => void;
    onclosecomposer: () => void;
    onsubmitissue: () => void;
    onissuetitlechange: (value: string) => void;
    onissuebodychange: (value: string) => void;
    onopenissue: (number: number) => void;
  }

  let {
    summary,
    composerOpen,
    issueTitle,
    issueBody,
    issueError = null,
    issueSubmitting = false,
    onviewprs,
    onviewissues,
    onopencomposer,
    onclosecomposer,
    onsubmitissue,
    onissuetitlechange,
    onissuebodychange,
    onopenissue,
  }: Props = $props();

  const key = $derived(repoKey(summary));
  const syncTime = $derived(
    summary.last_sync_completed_at
      || summary.last_sync_started_at,
  );
  const metrics = $derived<RepoMetric[]>([
    {
      label: "Open PRs",
      value: summary.open_pr_count,
      tone: "blue",
      onclick: onviewprs,
    },
    {
      label: "Draft PRs",
      value: summary.draft_pr_count,
      tone: "amber",
    },
    {
      label: "Open issues",
      value: summary.open_issue_count,
      tone: "green",
      onclick: onviewissues,
    },
    {
      label: "Cached PRs",
      value: summary.cached_pr_count,
    },
    {
      label: "Cached issues",
      value: summary.cached_issue_count,
    },
  ]);
</script>

<article class="repo-card" aria-labelledby={`repo-${key}`}>
  <div class="repo-card__header">
    <div class="repo-card__identity">
      <div class="repo-card__name-row">
        <button
          id={`repo-${key}`}
          class="repo-card__name"
          onclick={onviewprs}
        >
          {summary.owner}/{summary.name}
        </button>
        {#if summary.platform_host !== "github.com"}
          <Chip size="sm" class="chip--muted" uppercase={false}>
            {summary.platform_host}
          </Chip>
        {/if}
        {#if summary.last_sync_error}
          <Chip
            size="sm"
            class="chip--red"
            uppercase={false}
            title={summary.last_sync_error}
          >
            Sync error
          </Chip>
        {/if}
      </div>

      <div class="repo-card__meta">
        {#if summary.most_recent_activity_at}
          <span title={localDateTimeLabel(summary.most_recent_activity_at)}>
            Active {timeAgo(summary.most_recent_activity_at)}
          </span>
        {:else}
          <span>No cached activity</span>
        {/if}
        {#if syncTime}
          <span title={localDateTimeLabel(syncTime)}>
            Synced {timeAgo(syncTime)}
          </span>
        {/if}
      </div>
    </div>

    <div class="repo-card__actions">
      <ActionButton
        size="sm"
        tone="info"
        surface="soft"
        onclick={onopencomposer}
      >
        New issue
      </ActionButton>
    </div>
  </div>

  <RepoMetricGrid {metrics} compact />

  {#if summary.last_sync_error}
    <div class="repo-card__sync-error">
      <span>Last sync error</span>
      <p>{summary.last_sync_error}</p>
    </div>
  {/if}

  <div class="repo-card__body">
    <section class="repo-card__section">
      <div class="repo-card__section-head">
        <h2>Most active authors</h2>
        <span>{summary.active_authors.length} tracked</span>
      </div>
      {#if summary.active_authors.length > 0}
        <div class="repo-card__authors">
          {#each summary.active_authors as author (author.login)}
            <Chip size="sm" class="chip--muted" uppercase={false}>
              <strong>{author.login}</strong>
              <span>{author.item_count}</span>
            </Chip>
          {/each}
        </div>
      {:else}
        <p class="repo-card__empty-note">No cached authors yet.</p>
      {/if}
    </section>

    <section class="repo-card__section">
      <div class="repo-card__section-head">
        <h2>Recent open issues</h2>
        <span>{summary.open_issue_count} open</span>
      </div>
      {#if summary.recent_issues.length > 0}
        <div class="repo-card__issues">
          {#each summary.recent_issues as issue (issue.number)}
            <button
              class="repo-card__issue-row"
              onclick={() => onopenissue(issue.number)}
            >
              <span class="repo-card__issue-title">
                <strong>#{issue.number}</strong>
                {issue.title}
              </span>
              <span class="repo-card__issue-meta">
                {issue.author} · {timeAgo(issue.last_activity_at)}
              </span>
            </button>
          {/each}
        </div>
      {:else}
        <p class="repo-card__empty-note">No open issues in cache.</p>
      {/if}
    </section>
  </div>

  {#if composerOpen}
    <RepoIssueComposer
      title={issueTitle}
      body={issueBody}
      error={issueError}
      submitting={issueSubmitting}
      ontitlechange={onissuetitlechange}
      onbodychange={onissuebodychange}
      oncancel={onclosecomposer}
      {onsubmitissue}
    />
  {/if}
</article>

<style>
  .repo-card {
    overflow: hidden;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-lg);
    background: var(--bg-surface);
    box-shadow: var(--shadow-sm);
  }

  .repo-card__header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
    padding: 14px;
  }

  .repo-card__identity {
    min-width: 0;
  }

  .repo-card__name-row,
  .repo-card__meta,
  .repo-card__actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }

  .repo-card__name-row {
    margin-bottom: 6px;
  }

  .repo-card__name {
    color: var(--text-primary);
    font-size: 16px;
    font-weight: 600;
    line-height: 1.25;
  }

  .repo-card__name:hover {
    color: var(--accent-blue);
  }

  .repo-card__meta {
    color: var(--text-secondary);
    font-size: 12px;
  }

  .repo-card__actions {
    justify-content: flex-end;
  }

  .repo-card__sync-error {
    display: grid;
    gap: 4px;
    padding: 10px 14px;
    border-bottom: 1px solid var(--border-muted);
    background: color-mix(in srgb, var(--accent-red) 8%, transparent);
  }

  .repo-card__sync-error span {
    color: var(--accent-red);
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  .repo-card__sync-error p {
    color: var(--text-primary);
    font-size: 12px;
  }

  .repo-card__body {
    display: grid;
    gap: 16px;
    padding: 14px;
  }

  .repo-card__section {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .repo-card__section-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .repo-card__section-head h2 {
    color: var(--text-primary);
    font-size: 13px;
    font-weight: 600;
  }

  .repo-card__section-head span {
    color: var(--text-muted);
    font-size: 12px;
  }

  .repo-card__authors {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .repo-card__issues {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .repo-card__issue-row {
    width: 100%;
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
    padding: 8px 10px;
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-md);
    background: var(--bg-inset);
    text-align: left;
  }

  .repo-card__issue-row:hover {
    background: var(--bg-surface-hover);
  }

  .repo-card__issue-title {
    min-width: 0;
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  .repo-card__issue-title strong {
    margin-right: 6px;
    color: var(--accent-blue);
  }

  .repo-card__issue-meta {
    flex-shrink: 0;
    color: var(--text-secondary);
    font-size: 12px;
  }

  .repo-card__empty-note {
    color: var(--text-muted);
    font-size: 12px;
  }

  @media (max-width: 960px) {
    .repo-card__header {
      flex-direction: column;
    }

    .repo-card__actions {
      justify-content: flex-start;
    }
  }

  @media (max-width: 700px) {
    .repo-card__issue-row {
      align-items: flex-start;
      flex-direction: column;
    }

    .repo-card__issue-meta {
      white-space: normal;
    }
  }
</style>
