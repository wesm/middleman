<script lang="ts">
  import { onMount } from "svelte";
  import { getStores } from "@middleman/ui";
  import type {
    RepoSummary,
    RepoSummaryAuthor,
    RepoSummaryIssue,
  } from "@middleman/ui/api/types";

  import {
    apiErrorMessage,
    client,
  } from "../../api/runtime.js";
  import { setGlobalRepo } from "../../stores/filter.svelte.js";
  import { navigate } from "../../stores/router.svelte.js";

  type RepoSummaryCard = Omit<
    RepoSummary,
    "active_authors" | "recent_issues"
  > & {
    active_authors: RepoSummaryAuthor[];
    recent_issues: RepoSummaryIssue[];
  };

  const stores = getStores();

  let summaries = $state<RepoSummaryCard[]>([]);
  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let openComposerKey = $state<string | null>(null);
  let issueTitleByRepo = $state<Record<string, string>>({});
  let issueBodyByRepo = $state<Record<string, string>>({});
  let issueErrorByRepo = $state<
    Record<string, string | null>
  >({});
  let issueSubmittingByRepo = $state<Record<string, boolean>>(
    {},
  );

  const totals = $derived.by(() =>
    summaries.reduce(
      (acc, summary) => ({
        openPRs: acc.openPRs + summary.open_pr_count,
        openIssues:
          acc.openIssues + summary.open_issue_count,
      }),
      { openPRs: 0, openIssues: 0 },
    )
  );

  function repoKey(summary: {
    owner: string;
    name: string;
  }): string {
    return `${summary.owner}/${summary.name}`;
  }

  function timeAgo(dateStr: string): string {
    const diffMs = Date.now() - new Date(dateStr).getTime();
    const diffMin = Math.floor(diffMs / 60_000);
    if (diffMin < 1) return "just now";
    if (diffMin < 60) return `${diffMin}m ago`;
    const diffHr = Math.floor(diffMin / 60);
    if (diffHr < 24) return `${diffHr}h ago`;
    const days = Math.floor(diffHr / 24);
    if (days < 30) return `${days}d ago`;
    return `${Math.floor(days / 30)}mo ago`;
  }

  function toLocaleLabel(dateStr: string): string {
    return new Date(dateStr).toLocaleString();
  }

  function normalizeSummaries(
    data: RepoSummary[] | null | undefined,
  ): RepoSummaryCard[] {
    return (data ?? []).map((summary) => ({
      ...summary,
      active_authors: summary.active_authors ?? [],
      recent_issues: summary.recent_issues ?? [],
    }));
  }

  async function loadSummaries(): Promise<void> {
    const showSpinner = summaries.length === 0;
    if (showSpinner) loading = true;
    loadError = null;

    const { data, error } = await client.GET("/repos/summary");
    if (error) {
      loadError = apiErrorMessage(
        error,
        "failed to load repositories",
      );
      if (showSpinner) loading = false;
      return;
    }

    summaries = normalizeSummaries(data);
    loading = false;
  }

  function filterAndNavigate(
    summary: RepoSummaryCard,
    path: string,
  ): void {
    setGlobalRepo(repoKey(summary));
    navigate(path);
  }

  function openComposer(summary: RepoSummaryCard): void {
    const key = repoKey(summary);
    openComposerKey = key;
    issueErrorByRepo[key] = null;
    if (issueTitleByRepo[key] === undefined) {
      issueTitleByRepo[key] = "";
    }
    if (issueBodyByRepo[key] === undefined) {
      issueBodyByRepo[key] = "";
    }
  }

  function closeComposer(key: string): void {
    if (openComposerKey === key) {
      openComposerKey = null;
    }
    issueErrorByRepo[key] = null;
  }

  async function submitIssue(
    summary: RepoSummaryCard,
  ): Promise<void> {
    const key = repoKey(summary);
    const title = (issueTitleByRepo[key] ?? "").trim();
    if (title === "") {
      issueErrorByRepo[key] = "Title is required.";
      return;
    }

    issueSubmittingByRepo[key] = true;
    issueErrorByRepo[key] = null;

    const { data, error } = await client.POST(
      "/repos/{owner}/{name}/issues",
      {
        params: {
          path: {
            owner: summary.owner,
            name: summary.name,
          },
        },
        body: {
          title,
          body: issueBodyByRepo[key] ?? "",
        },
      },
    );

    issueSubmittingByRepo[key] = false;
    if (error || !data) {
      issueErrorByRepo[key] = apiErrorMessage(
        error,
        "failed to create issue",
      );
      return;
    }

    issueTitleByRepo[key] = "";
    issueBodyByRepo[key] = "";
    openComposerKey = null;
    setGlobalRepo(key);
    navigate(
      `/issues/${summary.owner}/${summary.name}/${data.Number}`,
    );
  }

  onMount(() => {
    void loadSummaries();
    const unsubscribe =
      stores.sync.subscribeSyncComplete(() => {
        void loadSummaries();
      });
    const refreshHandle = setInterval(() => {
      void loadSummaries();
    }, 30_000);
    return () => {
      unsubscribe();
      clearInterval(refreshHandle);
    };
  });
</script>

<section class="repo-page">
  <header class="repo-page__header">
    <div>
      <p class="repo-page__eyebrow">Overview</p>
      <h1 class="repo-page__title">Repositories</h1>
      <p class="repo-page__subtitle">
        Cached repo health, current workload, and the most active
        issue threads.
      </p>
    </div>
    <div class="repo-page__totals">
      <div class="repo-page__total-card">
        <span class="repo-page__total-value">
          {summaries.length}
        </span>
        <span class="repo-page__total-label">Tracked repos</span>
      </div>
      <div class="repo-page__total-card">
        <span class="repo-page__total-value">
          {totals.openPRs}
        </span>
        <span class="repo-page__total-label">Open PRs</span>
      </div>
      <div class="repo-page__total-card">
        <span class="repo-page__total-value">
          {totals.openIssues}
        </span>
        <span class="repo-page__total-label">Open issues</span>
      </div>
    </div>
  </header>

  {#if stores.settings.isSettingsLoaded() && !stores.settings.hasConfiguredRepos()}
    <div class="repo-page__empty">
      <h2>No repositories configured</h2>
      <p>
        Add a repository in Settings before using the repository
        overview.
      </p>
      <button
        class="repo-page__settings-btn"
        onclick={() => navigate("/settings")}
      >
        Open Settings
      </button>
    </div>
  {:else if loading}
    <div class="repo-page__empty">
      <h2>Loading repositories</h2>
      <p>Fetching the latest cached repo summaries.</p>
    </div>
  {:else if loadError}
    <div class="repo-page__empty repo-page__empty--error">
      <h2>Couldn’t load repositories</h2>
      <p>{loadError}</p>
      <button class="repo-page__settings-btn" onclick={() => void loadSummaries()}>
        Retry
      </button>
    </div>
  {:else if summaries.length === 0}
    <div class="repo-page__empty">
      <h2>No cached repositories yet</h2>
      <p>Run a sync to populate repository summaries.</p>
    </div>
  {:else}
    <div class="repo-grid">
      {#each summaries as summary (repoKey(summary))}
        {@const key = repoKey(summary)}
        {@const syncTime = summary.last_sync_completed_at || summary.last_sync_started_at}
        <article class="repo-card">
          <div class="repo-card__header">
            <div class="repo-card__identity">
              <div class="repo-card__name-row">
                <button
                  class="repo-card__name"
                  onclick={() => filterAndNavigate(summary, "/pulls")}
                >
                  {summary.owner}/{summary.name}
                </button>
                {#if summary.platform_host !== "github.com"}
                  <span class="repo-card__host">
                    {summary.platform_host}
                  </span>
                {/if}
              </div>
              <div class="repo-card__meta">
                {#if summary.most_recent_activity_at}
                  <span title={toLocaleLabel(summary.most_recent_activity_at)}>
                    Active {timeAgo(summary.most_recent_activity_at)}
                  </span>
                {:else}
                  <span>No cached activity</span>
                {/if}
                {#if syncTime}
                  <span title={toLocaleLabel(syncTime)}>
                    Synced {timeAgo(syncTime)}
                  </span>
                {/if}
                {#if summary.last_sync_error}
                  <span class="repo-card__sync-error">
                    Sync error
                  </span>
                {/if}
              </div>
            </div>

            <div class="repo-card__actions">
              <button
                class="repo-card__action"
                onclick={() => filterAndNavigate(summary, "/pulls")}
              >
                View PRs
              </button>
              <button
                class="repo-card__action"
                onclick={() => filterAndNavigate(summary, "/issues")}
              >
                View issues
              </button>
              <button
                class="repo-card__action repo-card__action--primary"
                onclick={() => openComposer(summary)}
              >
                New issue
              </button>
            </div>
          </div>

          <div class="repo-card__stats">
            <div class="repo-card__stat">
              <span class="repo-card__stat-value">
                {summary.open_pr_count}
              </span>
              <span class="repo-card__stat-label">Open PRs</span>
            </div>
            <div class="repo-card__stat">
              <span class="repo-card__stat-value">
                {summary.draft_pr_count}
              </span>
              <span class="repo-card__stat-label">Draft PRs</span>
            </div>
            <div class="repo-card__stat">
              <span class="repo-card__stat-value">
                {summary.open_issue_count}
              </span>
              <span class="repo-card__stat-label">Open issues</span>
            </div>
            <div class="repo-card__stat">
              <span class="repo-card__stat-value">
                {summary.cached_pr_count + summary.cached_issue_count}
              </span>
              <span class="repo-card__stat-label">Cached items</span>
            </div>
          </div>

          <div class="repo-card__body">
            <section class="repo-card__section">
              <div class="repo-card__section-head">
                <h2>Most active authors</h2>
                <span>
                  {summary.active_authors.length || 0} tracked
                </span>
              </div>
              {#if summary.active_authors.length > 0}
                <div class="repo-card__authors">
                  {#each summary.active_authors as author (author.login)}
                    <span class="repo-card__author-pill">
                      <strong>{author.login}</strong>
                      <span>{author.item_count}</span>
                    </span>
                  {/each}
                </div>
              {:else}
                <p class="repo-card__empty-note">
                  No cached authors yet.
                </p>
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
                      onclick={() =>
                        filterAndNavigate(
                          summary,
                          `/issues/${summary.owner}/${summary.name}/${issue.number}`,
                        )}
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
                <p class="repo-card__empty-note">
                  No open issues in cache.
                </p>
              {/if}
            </section>
          </div>

          {#if openComposerKey === key}
            <form
              class="repo-card__composer"
              onsubmit={(event) => {
                event.preventDefault();
                void submitIssue(summary);
              }}
            >
              <div class="repo-card__section-head">
                <h2>Create issue</h2>
                <button
                  type="button"
                  class="repo-card__dismiss"
                  onclick={() => closeComposer(key)}
                >
                  Cancel
                </button>
              </div>

              <input
                class="repo-card__input"
                type="text"
                placeholder="Issue title"
                bind:value={issueTitleByRepo[key]}
              />
              <textarea
                class="repo-card__textarea"
                rows="4"
                placeholder="Describe the problem, context, or follow-up work"
                bind:value={issueBodyByRepo[key]}
              ></textarea>

              {#if issueErrorByRepo[key]}
                <p class="repo-card__error">
                  {issueErrorByRepo[key]}
                </p>
              {/if}

              <div class="repo-card__composer-actions">
                <button
                  type="submit"
                  class="repo-card__action repo-card__action--primary"
                  disabled={issueSubmittingByRepo[key]}
                >
                  {issueSubmittingByRepo[key]
                    ? "Creating..."
                    : "Create issue"}
                </button>
              </div>
            </form>
          {/if}
        </article>
      {/each}
    </div>
  {/if}
</section>

<style>
  .repo-page {
    flex: 1;
    overflow-y: auto;
    padding: 24px;
    display: flex;
    flex-direction: column;
    gap: 20px;
    background:
      radial-gradient(circle at top right, rgba(37, 99, 235, 0.08), transparent 28%),
      linear-gradient(180deg, var(--bg-primary) 0%, color-mix(in srgb, var(--bg-primary) 84%, var(--bg-surface)) 100%);
  }

  .repo-page__header {
    display: flex;
    justify-content: space-between;
    gap: 20px;
    align-items: flex-start;
  }

  .repo-page__eyebrow {
    text-transform: uppercase;
    letter-spacing: 0.12em;
    font-size: 11px;
    color: var(--text-muted);
    margin-bottom: 8px;
  }

  .repo-page__title {
    font-size: 30px;
    line-height: 1;
    letter-spacing: -0.04em;
    margin-bottom: 8px;
  }

  .repo-page__subtitle {
    max-width: 560px;
    color: var(--text-secondary);
    font-size: 14px;
  }

  .repo-page__totals {
    display: grid;
    grid-template-columns: repeat(3, minmax(120px, 1fr));
    gap: 10px;
    min-width: min(100%, 420px);
  }

  .repo-page__total-card {
    background: color-mix(in srgb, var(--bg-surface) 88%, var(--accent-blue) 12%);
    border: 1px solid color-mix(in srgb, var(--border-default) 80%, var(--accent-blue) 20%);
    border-radius: 16px;
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    box-shadow: var(--shadow-sm);
  }

  .repo-page__total-value {
    font-size: 24px;
    font-weight: 700;
    letter-spacing: -0.04em;
  }

  .repo-page__total-label {
    color: var(--text-secondary);
    font-size: 12px;
  }

  .repo-page__empty {
    background: var(--bg-surface);
    border: 1px solid var(--border-default);
    border-radius: 18px;
    padding: 28px;
    box-shadow: var(--shadow-md);
    display: flex;
    flex-direction: column;
    gap: 10px;
    max-width: 520px;
  }

  .repo-page__empty h2 {
    font-size: 18px;
    letter-spacing: -0.02em;
  }

  .repo-page__empty p {
    color: var(--text-secondary);
  }

  .repo-page__empty--error {
    border-color: color-mix(in srgb, var(--accent-red) 40%, var(--border-default));
  }

  .repo-page__settings-btn {
    align-self: flex-start;
    padding: 8px 12px;
    border-radius: 999px;
    background: var(--text-primary);
    color: var(--bg-surface);
  }

  .repo-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
    gap: 16px;
  }

  .repo-card {
    background: color-mix(in srgb, var(--bg-surface) 95%, transparent);
    border: 1px solid color-mix(in srgb, var(--border-default) 85%, transparent);
    border-radius: 20px;
    box-shadow: var(--shadow-md);
    overflow: hidden;
    backdrop-filter: blur(8px);
  }

  .repo-card__header,
  .repo-card__body,
  .repo-card__composer {
    padding: 18px;
  }

  .repo-card__header {
    display: flex;
    justify-content: space-between;
    gap: 18px;
    border-bottom: 1px solid var(--border-muted);
  }

  .repo-card__identity {
    min-width: 0;
  }

  .repo-card__name-row {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    margin-bottom: 8px;
  }

  .repo-card__name {
    font-size: 20px;
    font-weight: 700;
    letter-spacing: -0.03em;
    color: var(--text-primary);
  }

  .repo-card__name:hover {
    color: var(--accent-blue);
  }

  .repo-card__host {
    font-size: 11px;
    color: var(--text-secondary);
    background: var(--bg-inset);
    border-radius: 999px;
    padding: 3px 7px;
  }

  .repo-card__meta {
    display: flex;
    flex-wrap: wrap;
    gap: 8px 12px;
    color: var(--text-secondary);
    font-size: 12px;
  }

  .repo-card__sync-error {
    color: var(--accent-red);
  }

  .repo-card__actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    justify-content: flex-end;
    align-content: flex-start;
  }

  .repo-card__action,
  .repo-card__dismiss {
    padding: 7px 12px;
    border-radius: 999px;
    border: 1px solid var(--border-default);
    background: var(--bg-surface);
    color: var(--text-secondary);
  }

  .repo-card__action:hover,
  .repo-card__dismiss:hover {
    border-color: var(--text-muted);
    color: var(--text-primary);
  }

  .repo-card__action--primary {
    background: var(--text-primary);
    color: var(--bg-surface);
    border-color: transparent;
  }

  .repo-card__action--primary:hover {
    background: color-mix(in srgb, var(--text-primary) 90%, var(--accent-blue) 10%);
    color: var(--bg-surface);
  }

  .repo-card__action:disabled {
    opacity: 0.65;
    cursor: not-allowed;
  }

  .repo-card__stats {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    border-bottom: 1px solid var(--border-muted);
  }

  .repo-card__stat {
    padding: 16px 18px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .repo-card__stat:not(:last-child) {
    border-right: 1px solid var(--border-muted);
  }

  .repo-card__stat-value {
    font-size: 22px;
    line-height: 1;
    font-weight: 700;
    letter-spacing: -0.04em;
  }

  .repo-card__stat-label {
    color: var(--text-secondary);
    font-size: 12px;
  }

  .repo-card__body {
    display: grid;
    gap: 18px;
  }

  .repo-card__section {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .repo-card__section-head {
    display: flex;
    justify-content: space-between;
    gap: 10px;
    align-items: center;
  }

  .repo-card__section-head h2 {
    font-size: 13px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--text-secondary);
  }

  .repo-card__section-head span {
    color: var(--text-muted);
    font-size: 12px;
  }

  .repo-card__authors {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .repo-card__author-pill {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 7px 10px;
    border-radius: 999px;
    background: var(--bg-inset);
    color: var(--text-secondary);
  }

  .repo-card__issues {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .repo-card__issue-row {
    width: 100%;
    text-align: left;
    display: flex;
    justify-content: space-between;
    gap: 12px;
    padding: 10px 12px;
    border-radius: 12px;
    background: var(--bg-inset);
    transition: transform 0.12s ease, background 0.12s ease;
  }

  .repo-card__issue-row:hover {
    background: var(--bg-surface-hover);
    transform: translateY(-1px);
  }

  .repo-card__issue-title {
    min-width: 0;
    display: inline-flex;
    gap: 8px;
    align-items: baseline;
    color: var(--text-primary);
  }

  .repo-card__issue-title strong {
    color: var(--accent-blue);
    flex-shrink: 0;
  }

  .repo-card__issue-meta {
    color: var(--text-secondary);
    font-size: 12px;
    white-space: nowrap;
  }

  .repo-card__empty-note {
    color: var(--text-muted);
  }

  .repo-card__composer {
    border-top: 1px solid var(--border-muted);
    background: color-mix(in srgb, var(--bg-inset) 82%, var(--bg-surface) 18%);
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .repo-card__input,
  .repo-card__textarea {
    width: 100%;
    background: var(--bg-surface);
  }

  .repo-card__textarea {
    resize: vertical;
    min-height: 104px;
  }

  .repo-card__error {
    color: var(--accent-red);
    font-size: 12px;
  }

  .repo-card__composer-actions {
    display: flex;
    justify-content: flex-end;
  }

  @media (max-width: 960px) {
    .repo-page {
      padding: 18px;
    }

    .repo-page__header {
      flex-direction: column;
    }

    .repo-page__totals {
      width: 100%;
      min-width: 0;
    }

    .repo-card__header {
      flex-direction: column;
    }

    .repo-card__actions {
      justify-content: flex-start;
    }
  }

  @media (max-width: 700px) {
    .repo-page__totals,
    .repo-card__stats {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .repo-card__stat:nth-child(2) {
      border-right: none;
    }

    .repo-card__issue-row {
      flex-direction: column;
    }

    .repo-card__issue-meta {
      white-space: normal;
    }
  }
</style>
