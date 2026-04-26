<script lang="ts">
  import { ActionButton, Chip } from "@middleman/ui";
  import { timeAgo } from "@middleman/ui/utils/time";
  import RepoMetricGrid from "./RepoMetricGrid.svelte";
  import {
    displayReleaseName,
    isStaleRelease,
    localDateTimeLabel,
    repoKey,
    shortDateLabel,
    type RepoMetric,
    type RepoSummaryCard,
  } from "./repoSummary.js";

  interface Props {
    summary: RepoSummaryCard;
    onviewprs: () => void;
    onviewissues: () => void;
    onopencomposer: () => void;
    onopenissue: (number: number) => void;
  }

  let {
    summary,
    onviewprs,
    onviewissues,
    onopencomposer,
    onopenissue,
  }: Props = $props();

  const key = $derived(repoKey(summary));
  const syncTime = $derived(
    summary.last_sync_completed_at
      || summary.last_sync_started_at,
  );
  const release = $derived(summary.latest_release);
  const staleRelease = $derived(isStaleRelease(summary));
  const releaseDate = $derived(release?.published_at);
  const releaseLabel = $derived(displayReleaseName(release));
  const releaseStatus = $derived.by(() => {
    if (!release) return "No release";
    if (staleRelease) return "Stale";
    return release.prerelease ? "Pre-release" : "Latest";
  });
  const releaseStatusClass = $derived.by(() => {
    if (!release) return "chip--muted";
    if (staleRelease) return "chip--red";
    return release.prerelease ? "chip--amber" : "chip--green";
  });
  const activityPoints = $derived.by(() =>
    [...summary.commit_timeline].reverse(),
  );
  const metrics = $derived<RepoMetric[]>([
    {
      label: "Open PRs",
      value: summary.open_pr_count,
      tone: "blue",
      onclick: onviewprs,
    },
    {
      label: "Open issues",
      value: summary.open_issue_count,
      tone: "green",
      onclick: onviewissues,
    },
    {
      label: "Draft PRs",
      value: summary.draft_pr_count,
      tone: "amber",
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

  function timelinePosition(committedAt: string): number {
    if (!releaseDate) return 50;
    const start = new Date(releaseDate).getTime();
    const end = Date.now();
    const current = new Date(committedAt).getTime();
    if (!Number.isFinite(start) || !Number.isFinite(current) || end <= start) {
      return 50;
    }
    const pct = ((current - start) / (end - start)) * 100;
    return Math.max(1, Math.min(99, pct));
  }

  function authorInitial(author: string): string {
    return (author.trim()[0] ?? "?").toUpperCase();
  }
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
          <span>{summary.owner}</span>
          <span class="repo-card__slash">/</span>
          <span>{summary.name}</span>
        </button>
      </div>
      <Chip size="sm" class="chip--muted" uppercase={false}>
        {summary.platform_host}
      </Chip>
    </div>

    <div class="repo-card__actions">
      <ActionButton
        size="sm"
        tone="neutral"
        surface="outline"
        onclick={onopencomposer}
      >
        New issue
      </ActionButton>
    </div>
  </div>

  <RepoMetricGrid {metrics} compact />

  <section class="repo-card__release" aria-label="Latest release">
    <div class="repo-card__release-head">
      <span>Latest release</span>
      {#if summary.commits_since_release !== undefined}
        <strong>
          {summary.commits_since_release} {summary.commits_since_release === 1 ? "commit" : "commits"}
        </strong>
      {/if}
    </div>

    <div class="repo-card__release-meta">
      <Chip size="md" class="chip--release" uppercase={false}>
        {releaseLabel}
      </Chip>
      <Chip
        size="sm"
        class={releaseStatusClass}
        uppercase={false}
      >
        {releaseStatus}
      </Chip>
      {#if releaseDate}
        <span title={localDateTimeLabel(releaseDate)}>
          {timeAgo(releaseDate)}
        </span>
      {/if}
      {#if summary.commits_since_release !== undefined}
        <span class="repo-card__commits-copy">since release</span>
      {/if}
    </div>

    <div class="repo-card__timeline">
      <div class="repo-card__timeline-track">
        {#each activityPoints as point (point.sha)}
          <span
            class={[
              "repo-card__timeline-point",
              {
                "repo-card__timeline-point--stale": staleRelease,
                "repo-card__timeline-point--pre": release?.prerelease,
              },
            ]}
            style={`--x: ${timelinePosition(point.committed_at)}%;`}
            title={localDateTimeLabel(point.committed_at)}
          ></span>
        {/each}
      </div>
      <div class="repo-card__timeline-labels">
        <span>{releaseDate ? shortDateLabel(releaseDate) : "Release"}</span>
        <span>Now</span>
      </div>
    </div>
  </section>

  <section class="repo-card__issues" aria-label="Recent open issues">
    <h2>Recent open issues</h2>
    {#if summary.recent_issues.length > 0}
      <div class="repo-card__issue-list">
        {#each summary.recent_issues as issue (issue.number)}
          <button
            class="repo-card__issue-row"
            onclick={() => onopenissue(issue.number)}
          >
            <span class="repo-card__issue-main">
              <strong>#{issue.number}</strong>
              <span>{issue.title}</span>
            </span>
            <span class="repo-card__issue-meta">
              <span
                class="repo-card__avatar"
                title={issue.author}
                aria-label={issue.author}
              >
                {authorInitial(issue.author)}
              </span>
              <span>{timeAgo(issue.last_activity_at)}</span>
            </span>
          </button>
        {/each}
      </div>
    {:else}
      <p class="repo-card__empty-note">No open issues in cache.</p>
    {/if}
  </section>

  <footer class="repo-card__footer">
    <span
      class={[
        "repo-card__status",
        { "repo-card__status--error": summary.last_sync_error },
      ]}
    >
      {summary.last_sync_error ? "Sync issue" : "Active"}
    </span>
    {#if syncTime}
      <span title={localDateTimeLabel(syncTime)}>
        Synced {timeAgo(syncTime)}
      </span>
    {:else}
      <span>Not synced yet</span>
    {/if}
  </footer>
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
    gap: 14px;
    padding: 14px 14px 10px;
  }

  .repo-card__identity {
    min-width: 0;
    display: grid;
    gap: 4px;
    justify-items: start;
  }

  .repo-card__name-row,
  .repo-card__actions,
  .repo-card__release-meta,
  .repo-card__footer,
  .repo-card__issue-meta {
    display: flex;
    align-items: center;
  }

  .repo-card__name-row {
    min-width: 0;
    gap: 6px;
    color: var(--text-muted);
  }

  .repo-card__name {
    min-width: 0;
    color: var(--text-primary);
    font-size: 15px;
    font-weight: 700;
    line-height: 1.25;
    overflow-wrap: anywhere;
  }

  .repo-card__name:hover {
    color: var(--accent-blue);
  }

  .repo-card__slash {
    padding: 0 4px;
    color: var(--text-muted);
    font-weight: 500;
  }

  .repo-card__actions {
    flex-shrink: 0;
    gap: 8px;
  }

  .repo-card__release {
    display: grid;
    gap: 8px;
    padding: 12px 14px 10px;
    border-bottom: 1px solid var(--border-muted);
  }

  .repo-card__release-head {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    color: var(--text-secondary);
    font-size: 11px;
    line-height: 1.2;
  }

  .repo-card__release-head strong {
    color: var(--text-primary);
    font-size: 12px;
    text-align: right;
  }

  .repo-card__release-meta {
    flex-wrap: wrap;
    gap: 8px;
    color: var(--text-secondary);
    font-size: 12px;
  }

  .repo-card__commits-copy {
    margin-left: -4px;
    color: var(--text-muted);
  }

  .repo-card__timeline {
    display: grid;
    gap: 4px;
  }

  .repo-card__timeline-track {
    position: relative;
    height: 16px;
  }

  .repo-card__timeline-track::before {
    content: "";
    position: absolute;
    top: 7px;
    right: 0;
    left: 0;
    height: 2px;
    border-radius: 999px;
    background: var(--border-muted);
  }

  .repo-card__timeline-point {
    position: absolute;
    top: 5px;
    left: var(--x);
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent-green);
    transform: translateX(-50%);
    box-shadow: 0 0 0 2px var(--bg-surface);
  }

  .repo-card__timeline-point--pre {
    background: var(--accent-amber);
  }

  .repo-card__timeline-point--stale {
    background: var(--accent-red);
  }

  .repo-card__timeline-labels {
    display: flex;
    justify-content: space-between;
    color: var(--text-secondary);
    font-size: 11px;
  }

  .repo-card__issues {
    display: grid;
    gap: 8px;
    padding: 10px 14px 12px;
    min-height: 82px;
  }

  .repo-card__issues h2 {
    color: var(--text-secondary);
    font-size: 12px;
    font-weight: 500;
  }

  .repo-card__issue-list {
    display: grid;
    gap: 4px;
  }

  .repo-card__issue-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    min-height: 24px;
    text-align: left;
  }

  .repo-card__issue-row:hover .repo-card__issue-main span {
    color: var(--accent-blue);
  }

  .repo-card__issue-main {
    min-width: 0;
    display: flex;
    align-items: baseline;
    gap: 8px;
    color: var(--text-primary);
  }

  .repo-card__issue-main strong {
    flex-shrink: 0;
    color: var(--accent-blue);
    font-weight: 600;
  }

  .repo-card__issue-main span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .repo-card__issue-meta {
    flex-shrink: 0;
    gap: 8px;
    color: var(--text-secondary);
    font-size: 12px;
  }

  .repo-card__avatar {
    display: inline-grid;
    width: 18px;
    height: 18px;
    place-items: center;
    border: 1px solid var(--border-default);
    border-radius: 50%;
    background: var(--bg-inset);
    color: var(--text-secondary);
    font-size: 10px;
    font-weight: 700;
  }

  .repo-card__empty-note {
    color: var(--text-muted);
    font-size: 12px;
  }

  .repo-card__footer {
    gap: 10px;
    padding: 9px 14px;
    border-top: 1px solid var(--border-muted);
    color: var(--text-muted);
    font-size: 12px;
  }

  .repo-card__status {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--accent-green);
    font-weight: 600;
  }

  .repo-card__status::before {
    content: "";
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: currentColor;
  }

  .repo-card__status--error {
    color: var(--accent-red);
  }

  :global(.chip--release) {
    background: color-mix(in srgb, var(--accent-blue) 13%, transparent);
    color: var(--accent-blue);
  }

  @media (max-width: 700px) {
    .repo-card__header {
      flex-direction: column;
    }

    .repo-card__actions {
      width: 100%;
      justify-content: space-between;
    }

    .repo-card__issue-row {
      align-items: flex-start;
      flex-direction: column;
      gap: 4px;
    }

    .repo-card__issue-main span {
      white-space: normal;
    }
  }
</style>
