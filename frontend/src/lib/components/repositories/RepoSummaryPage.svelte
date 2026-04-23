<script lang="ts">
  import { onMount } from "svelte";
  import { getStores } from "@middleman/ui";
  import type { RepoSummary } from "@middleman/ui/api/types";

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
    type RepoMetric,
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

  const overviewMetrics = $derived<RepoMetric[]>([
    {
      label: "Tracked repos",
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
  ]);

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
      <p class="repo-page__eyebrow">Overview</p>
      <h1 class="repo-page__title">Repositories</h1>
      <p class="repo-page__subtitle">
        Cached repo health, current workload, and the most active
        issue threads.
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
    <div class="repo-grid">
      {#each summaries as summary (repoKey(summary))}
        {@const key = repoKey(summary)}
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
    gap: 16px;
    overflow-y: auto;
    padding: 24px;
    background: var(--bg-primary);
  }

  .repo-page__header {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(320px, 420px);
    gap: 20px;
    align-items: start;
  }

  .repo-page__eyebrow {
    margin-bottom: 4px;
    color: var(--text-muted);
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .repo-page__title {
    margin-bottom: 6px;
    color: var(--text-primary);
    font-size: 20px;
    font-weight: 600;
    line-height: 1.2;
  }

  .repo-page__subtitle {
    max-width: 560px;
    color: var(--text-secondary);
    font-size: 13px;
  }

  .repo-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(360px, 1fr));
    gap: 12px;
  }

  @media (max-width: 960px) {
    .repo-page {
      padding: 18px;
    }

    .repo-page__header {
      grid-template-columns: 1fr;
    }
  }

  @media (max-width: 520px) {
    .repo-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
