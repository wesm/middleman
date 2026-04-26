<script lang="ts">
  import { onMount } from "svelte";
  import { getStores } from "@middleman/ui";
  import type { RepoSummary } from "@middleman/ui/api/types";

  import {
    ChevronDownIcon,
    RefreshIcon,
    SearchIcon,
  } from "../../icons.js";
  import {
    apiErrorMessage,
    client,
  } from "../../api/runtime.js";
  import { setGlobalRepo } from "../../stores/filter.svelte.js";
  import { navigate } from "../../stores/router.svelte.js";
  import RepoMetricGrid from "./RepoMetricGrid.svelte";
  import RepoPageState from "./RepoPageState.svelte";
  import RepoSummaryCard from "./RepoSummaryCard.svelte";
  import RepoIssueModal from "./RepoIssueModal.svelte";
  import {
    normalizeSummaries,
    repoKey,
    isStaleRelease,
    type RepoFilter,
    type RepoMetric,
    type RepoSort,
    type RepoSummaryCard as RepoSummaryCardData,
  } from "./repoSummary.js";

  const stores = getStores();

  let summaries = $state<RepoSummaryCardData[]>([]);
  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let composerSummary = $state<RepoSummaryCardData | null>(null);
  let issueTitleByRepo = $state<Record<string, string>>({});
  let issueBodyByRepo = $state<Record<string, string>>({});
  let issueErrorByRepo = $state<
    Record<string, string | null>
  >({});
  let issueSubmittingByRepo = $state<Record<string, boolean>>(
    {},
  );
  let searchQuery = $state("");
  let activeFilter = $state<RepoFilter>("all");
  let sortMode = $state<RepoSort>("name");

  const totals = $derived.by(() =>
    summaries.reduce(
      (acc, summary) => ({
        openPRs: acc.openPRs + summary.open_pr_count,
        openIssues:
          acc.openIssues + summary.open_issue_count,
        draftPRs: acc.draftPRs + summary.draft_pr_count,
        staleReleases: acc.staleReleases + (isStaleRelease(summary) ? 1 : 0),
      }),
      { openPRs: 0, openIssues: 0, draftPRs: 0, staleReleases: 0 },
    )
  );

  const overviewMetrics = $derived<RepoMetric[]>([
    {
      label: "Total repos",
      value: summaries.length,
    },
    {
      label: "Open PRs",
      value: totals.openPRs,
      tone: "blue",
    },
    {
      label: "Open issues",
      value: totals.openIssues,
      tone: "green",
    },
    {
      label: "Draft PRs",
      value: totals.draftPRs,
      tone: "amber",
    },
    {
      label: "Stale releases",
      value: totals.staleReleases,
      tone: "red",
    },
  ]);

  const filterCounts = $derived({
    all: summaries.length,
    prs: summaries.filter((summary) => summary.open_pr_count > 0).length,
    issues: summaries.filter((summary) => summary.open_issue_count > 0).length,
    stale: summaries.filter((summary) => isStaleRelease(summary)).length,
  });

  const filteredSummaries = $derived.by(() => {
    const q = searchQuery.trim().toLowerCase();
    const matches = summaries.filter((summary) => {
      if (activeFilter === "prs" && summary.open_pr_count === 0) return false;
      if (activeFilter === "issues" && summary.open_issue_count === 0) return false;
      if (activeFilter === "stale" && !isStaleRelease(summary)) return false;
      if (q === "") return true;
      return repoKey(summary).toLowerCase().includes(q)
        || summary.platform_host.toLowerCase().includes(q);
    });

    return [...matches].sort((a, b) => {
      switch (sortMode) {
        case "open-prs":
          return b.open_pr_count - a.open_pr_count || repoKey(a).localeCompare(repoKey(b));
        case "open-issues":
          return b.open_issue_count - a.open_issue_count || repoKey(a).localeCompare(repoKey(b));
        case "activity":
          return dateValue(b.most_recent_activity_at) - dateValue(a.most_recent_activity_at)
            || repoKey(a).localeCompare(repoKey(b));
        case "stale":
          return (b.commits_since_release ?? -1) - (a.commits_since_release ?? -1)
            || repoKey(a).localeCompare(repoKey(b));
        case "name":
        default:
          return repoKey(a).localeCompare(repoKey(b));
      }
    });
  });

  function dateValue(value: string | undefined): number {
    if (!value) return 0;
    return new Date(value).getTime();
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

    summaries = normalizeSummaries(data as RepoSummary[] | null);
    loading = false;
  }

  async function refreshSummaries(): Promise<void> {
    await client.POST("/sync");
    await loadSummaries();
  }

  function setFilter(filter: RepoFilter): void {
    activeFilter = filter;
  }

  function updateSearch(event: Event): void {
    searchQuery = (event.currentTarget as HTMLInputElement).value;
  }

  function updateSort(event: Event): void {
    sortMode = (event.currentTarget as HTMLSelectElement).value as RepoSort;
  }

  function filterAndNavigate(
    summary: RepoSummaryCardData,
    path: string,
  ): void {
    setGlobalRepo(repoKey(summary));
    navigate(path);
  }

  function openComposer(summary: RepoSummaryCardData): void {
    const key = repoKey(summary);
    composerSummary = summary;
    issueErrorByRepo[key] = null;
    if (issueTitleByRepo[key] === undefined) {
      issueTitleByRepo[key] = "";
    }
    if (issueBodyByRepo[key] === undefined) {
      issueBodyByRepo[key] = "";
    }
  }

  function closeComposer(key: string): void {
    if (composerSummary && repoKey(composerSummary) === key) {
      composerSummary = null;
    }
    issueErrorByRepo[key] = null;
  }

  function updateIssueTitle(
    key: string,
    title: string,
  ): void {
    issueTitleByRepo[key] = title;
  }

  function updateIssueBody(key: string, body: string): void {
    issueBodyByRepo[key] = body;
  }

  async function submitIssue(
    summary: RepoSummaryCardData,
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
    composerSummary = null;
    setGlobalRepo(key);
    navigate(
      `/issues/${summary.owner}/${summary.name}/${data.Number}`,
    );
  }

  function submitActiveIssue(): void {
    if (!composerSummary) return;
    void submitIssue(composerSummary);
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
      <h1 class="repo-page__title">Repositories</h1>
      <p class="repo-page__subtitle">
        Summary of your tracked GitHub repositories
      </p>
    </div>
    <RepoMetricGrid metrics={overviewMetrics} />
  </header>

  {#if stores.settings.isSettingsLoaded() && !stores.settings.hasConfiguredRepos()}
    <RepoPageState
      title="No repositories configured"
      message="Add a repository in Settings before using the repository overview."
      actionLabel="Open Settings"
      onaction={() => navigate("/settings")}
    />
  {:else if loading}
    <RepoPageState
      title="Loading repositories"
      message="Fetching the latest cached repo summaries."
    />
  {:else if loadError}
    <RepoPageState
      title="Couldn’t load repositories"
      message={loadError}
      tone="error"
      actionLabel="Retry"
      onaction={() => void loadSummaries()}
    />
  {:else if summaries.length === 0}
    <RepoPageState
      title="No cached repositories yet"
      message="Run a sync to populate repository summaries."
    />
  {:else}
    <div class="repo-page__toolbar">
      <label class="repo-page__search">
        <SearchIcon size={16} aria-hidden="true" />
        <input
          value={searchQuery}
          placeholder="Filter repositories"
          oninput={updateSearch}
        />
      </label>

      <div class="repo-page__filters" aria-label="Repository filters">
        <button
          type="button"
          class={[
            "repo-page__filter",
            { "repo-page__filter--active": activeFilter === "all" },
          ]}
          onclick={() => setFilter("all")}
        >
          All <span>{filterCounts.all}</span>
        </button>
        <button
          type="button"
          class={[
            "repo-page__filter",
            { "repo-page__filter--active": activeFilter === "prs" },
          ]}
          onclick={() => setFilter("prs")}
        >
          Has PRs <span>{filterCounts.prs}</span>
        </button>
        <button
          type="button"
          class={[
            "repo-page__filter",
            { "repo-page__filter--active": activeFilter === "issues" },
          ]}
          onclick={() => setFilter("issues")}
        >
          Has issues <span>{filterCounts.issues}</span>
        </button>
        <button
          type="button"
          class={[
            "repo-page__filter",
            { "repo-page__filter--active": activeFilter === "stale" },
          ]}
          onclick={() => setFilter("stale")}
        >
          Stale release <span>{filterCounts.stale}</span>
        </button>
      </div>

      <div class="repo-page__sort">
        <label>
          <span>Sort by:</span>
          <select value={sortMode} onchange={updateSort}>
            <option value="name">Name</option>
            <option value="open-prs">Open PRs</option>
            <option value="open-issues">Open issues</option>
            <option value="activity">Recent activity</option>
            <option value="stale">Stale release</option>
          </select>
          <ChevronDownIcon size={14} aria-hidden="true" />
        </label>
        <span class="repo-page__results">
          {filteredSummaries.length} {filteredSummaries.length === 1 ? "result" : "results"}
        </span>
        <button
          type="button"
          class="repo-page__refresh"
          title="Refresh repositories"
          aria-label="Refresh repositories"
          onclick={() => void refreshSummaries()}
        >
          <RefreshIcon size={16} aria-hidden="true" />
        </button>
      </div>
    </div>

    {#if filteredSummaries.length === 0}
      <RepoPageState
        title="No repositories match"
        message="Adjust the filters or search query to see more repositories."
      />
    {:else}
    <div class="repo-grid">
      {#each filteredSummaries as summary (repoKey(summary))}
        <RepoSummaryCard
          {summary}
          onviewprs={() =>
            filterAndNavigate(summary, "/pulls")}
          onviewissues={() =>
            filterAndNavigate(summary, "/issues")}
          onopencomposer={() => openComposer(summary)}
          onopenissue={(number) =>
            filterAndNavigate(
              summary,
              `/issues/${summary.owner}/${summary.name}/${number}`,
            )}
        />
      {/each}
    </div>
    {/if}
  {/if}

  {#if composerSummary}
    {@const key = repoKey(composerSummary)}
    <RepoIssueModal
      summary={composerSummary}
      title={issueTitleByRepo[key] ?? ""}
      body={issueBodyByRepo[key] ?? ""}
      error={issueErrorByRepo[key] ?? null}
      submitting={issueSubmittingByRepo[key] ?? false}
      ontitlechange={(title) => updateIssueTitle(key, title)}
      onbodychange={(body) => updateIssueBody(key, body)}
      oncancel={() => closeComposer(key)}
      onsubmitissue={submitActiveIssue}
    />
  {/if}
</section>

<style>
  .repo-page {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 18px;
    overflow-y: auto;
    padding: 26px 28px;
    background: var(--bg-primary);
  }

  .repo-page__header {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(560px, 720px);
    gap: 20px;
    align-items: start;
    padding-bottom: 20px;
    border-bottom: 1px solid var(--border-muted);
  }

  .repo-page__title {
    margin-bottom: 6px;
    color: var(--text-primary);
    font-size: 22px;
    font-weight: 700;
    line-height: 1.2;
  }

  .repo-page__subtitle {
    max-width: 560px;
    color: var(--text-secondary);
    font-size: 13px;
  }

  .repo-page__toolbar {
    display: grid;
    grid-template-columns: minmax(220px, 360px) minmax(0, 1fr) auto;
    gap: 12px;
    align-items: center;
  }

  .repo-page__search {
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 8px;
    height: 36px;
    padding: 0 12px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    background: var(--bg-surface);
    color: var(--text-muted);
    box-shadow: var(--shadow-sm);
  }

  .repo-page__search input {
    width: 100%;
    min-width: 0;
    padding: 0;
    border: 0;
    background: transparent;
    color: var(--text-primary);
  }

  .repo-page__search input:focus {
    border: 0;
  }

  .repo-page__filters,
  .repo-page__sort,
  .repo-page__sort label {
    display: flex;
    align-items: center;
  }

  .repo-page__filters {
    flex-wrap: wrap;
    gap: 8px;
  }

  .repo-page__filter {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    height: 34px;
    padding: 0 12px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    background: var(--bg-surface);
    color: var(--text-primary);
    font-size: 13px;
    font-weight: 500;
  }

  .repo-page__filter:hover {
    background: var(--bg-surface-hover);
  }

  .repo-page__filter--active {
    border-color: var(--accent-blue);
    background: color-mix(in srgb, var(--accent-blue) 10%, var(--bg-surface));
    color: var(--accent-blue);
  }

  .repo-page__filter span {
    display: inline-grid;
    min-width: 20px;
    height: 18px;
    place-items: center;
    padding: 0 5px;
    border-radius: 9px;
    background: var(--bg-inset);
    color: inherit;
    font-size: 11px;
    font-weight: 700;
  }

  .repo-page__sort {
    justify-content: flex-end;
    gap: 12px;
  }

  .repo-page__sort label {
    position: relative;
    gap: 6px;
    height: 34px;
    padding: 0 30px 0 12px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    background: var(--bg-surface);
    color: var(--text-primary);
    box-shadow: var(--shadow-sm);
  }

  .repo-page__sort label > span {
    color: var(--text-secondary);
    font-size: 12px;
  }

  .repo-page__sort select {
    appearance: none;
    border: 0;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-weight: 600;
    outline: 0;
  }

  .repo-page__sort label :global(svg) {
    position: absolute;
    right: 10px;
    color: var(--text-secondary);
    pointer-events: none;
  }

  .repo-page__results {
    color: var(--text-secondary);
    font-size: 12px;
    white-space: nowrap;
  }

  .repo-page__refresh {
    display: inline-grid;
    width: 34px;
    height: 34px;
    place-items: center;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    background: var(--bg-surface);
    color: var(--text-secondary);
    box-shadow: var(--shadow-sm);
  }

  .repo-page__refresh:hover {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  .repo-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(390px, 1fr));
    gap: 12px;
  }

  @media (max-width: 960px) {
    .repo-page {
      padding: 18px;
    }

    .repo-page__header {
      grid-template-columns: 1fr;
    }

    .repo-page__toolbar {
      grid-template-columns: 1fr;
    }

    .repo-page__sort {
      justify-content: flex-start;
    }
  }

  @media (max-width: 520px) {
    .repo-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
