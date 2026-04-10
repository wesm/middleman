<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { getStores, getNavigate } from "../context.js";

  const { stacks, settings, sync } = getStores();
  const navigate = getNavigate();

  let unsubSync: (() => void) | undefined;

  onMount(() => {
    void stacks.loadStacks();
    unsubSync = sync.subscribeSyncComplete(() => void stacks.loadStacks());
  });

  onDestroy(() => {
    unsubSync?.();
  });

  let expandedRepos = $state<Set<string>>(new Set());
  let expandedStacks = $state<Set<number>>(new Set());

  // Expand all repos by default when data arrives
  $effect(() => {
    const byRepo = stacks.getStacksByRepo();
    if (byRepo.size > 0 && expandedRepos.size === 0) {
      expandedRepos = new Set(byRepo.keys());
    }
  });

  function toggleRepo(repo: string): void {
    const next = new Set(expandedRepos);
    if (next.has(repo)) next.delete(repo);
    else next.add(repo);
    expandedRepos = next;
  }

  function toggleStack(id: number): void {
    const next = new Set(expandedStacks);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    expandedStacks = next;
  }

  function getDotColor(member: {
    state: string;
    ci_status: string;
    review_decision: string;
  }): { color: string; outline: boolean } {
    if (member.state === "merged") {
      return { color: "#8b949e", outline: false };
    }
    if (member.ci_status === "failure") {
      return { color: "#f85149", outline: false };
    }
    if (member.ci_status === "pending" || member.review_decision === "CHANGES_REQUESTED") {
      return { color: "#d29922", outline: false };
    }
    if (
      member.state === "open" &&
      member.ci_status === "success" &&
      member.review_decision === "APPROVED"
    ) {
      return { color: "#238636", outline: false };
    }
    // No CI/review data
    return { color: "#21262d", outline: true };
  }

  function getHealthBadge(
    health: string,
    members: { state: string }[],
  ): { text: string; cls: string } {
    switch (health) {
      case "all_green":
        return { text: "All green", cls: "badge-green" };
      case "base_ready":
        return { text: "Base ready", cls: "badge-green" };
      case "partial_merge": {
        const merged = members.filter((m) => m.state === "merged").length;
        return { text: `${merged}/${members.length} merged`, cls: "badge-yellow" };
      }
      case "in_progress":
        return { text: "In progress", cls: "badge-yellow" };
      case "blocked":
        return { text: "Blocked", cls: "badge-red" };
      default:
        return { text: health, cls: "badge-yellow" };
    }
  }

  const totalStacks = $derived(stacks.getStacks().length);
  const byRepo = $derived(stacks.getStacksByRepo());
</script>

<div class="stacks-view">
  {#if stacks.isLoading() && totalStacks === 0}
    <div class="empty-state">
      <svg class="loading-spinner" width="18" height="18" viewBox="0 0 18 18" fill="none">
        <circle cx="9" cy="9" r="7" stroke="currentColor" stroke-opacity="0.2" stroke-width="2" />
        <path d="M16 9a7 7 0 0 0-7-7" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
      </svg>
      Loading stacks...
    </div>
  {:else if settings.isSettingsLoaded() && !settings.hasConfiguredRepos()}
    <div class="empty-state">
      No repositories configured. Add repos in config to detect stacks.
    </div>
  {:else if stacks.getError()}
    <div class="empty-state error">{stacks.getError()}</div>
  {:else if totalStacks === 0}
    <div class="empty-state">No stacks detected.</div>
  {:else}
    <div class="stacks-header">
      <h2 class="stacks-title">Stacks</h2>
      <span class="stacks-count">{totalStacks}</span>
    </div>

    <div class="stacks-body">
      {#each [...byRepo.entries()] as [repo, repoStacks]}
        <section class="repo-section">
          <button class="repo-header" onclick={() => toggleRepo(repo)}>
            <svg
              class="chevron"
              class:chevron-open={expandedRepos.has(repo)}
              width="12"
              height="12"
              viewBox="0 0 12 12"
              fill="none"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <polyline points="4,2 8,6 4,10" />
            </svg>
            <span class="repo-name">{repo}</span>
            <span class="repo-badge">{repoStacks.length}</span>
          </button>

          {#if expandedRepos.has(repo)}
            <div class="repo-stacks">
              {#each repoStacks as stack}
                {@const badge = getHealthBadge(stack.health, stack.members)}
                {@const isExpanded = expandedStacks.has(stack.id)}
                <div class="stack-card">
                  <button class="stack-header" onclick={() => toggleStack(stack.id)}>
                    <svg
                      class="chevron"
                      class:chevron-open={isExpanded}
                      width="12"
                      height="12"
                      viewBox="0 0 12 12"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="1.5"
                      stroke-linecap="round"
                      stroke-linejoin="round"
                    >
                      <polyline points="4,2 8,6 4,10" />
                    </svg>
                    <span class="stack-name">{stack.name}</span>
                    <span class="stack-pr-count">{stack.members.length} PRs</span>
                    <span class="health-dots">
                      {#each stack.members as member}
                        {@const dot = getDotColor(member)}
                        <span
                          class="dot"
                          class:dot-outline={dot.outline}
                          class:dot-dimmed={member.blocked_by != null}
                          style:background={dot.outline ? "transparent" : dot.color}
                          style:border-color={dot.outline ? dot.color : "transparent"}
                          title="#{member.number} {member.title}"
                        ></span>
                      {/each}
                    </span>
                    <span class="health-badge {badge.cls}">{badge.text}</span>
                  </button>

                  {#if isExpanded}
                    <div class="stack-members">
                      {#each stack.members as member, i}
                        {@const dot = getDotColor(member)}
                        <div
                          class="member-row"
                          class:member-dimmed={member.blocked_by != null}
                        >
                          <div class="member-chain">
                            <span
                              class="chain-dot"
                              class:dot-outline={dot.outline}
                              style:background={dot.outline ? "transparent" : dot.color}
                              style:border-color={dot.outline ? dot.color : "transparent"}
                            ></span>
                            {#if i < stack.members.length - 1}
                              <span class="chain-line"></span>
                            {/if}
                          </div>
                          <div class="member-info">
                            <button
                              class="member-link"
                              onclick={() => navigate(`/pulls/${stack.repo_owner}/${stack.repo_name}/${member.number}`)}
                            >
                              #{member.number}
                            </button>
                            <span class="member-title">{member.title}</span>
                            {#if member.is_draft}
                              <span class="draft-badge">Draft</span>
                            {/if}
                            {#if member.state === "merged"}
                              <span class="merged-badge">Merged</span>
                            {/if}
                          </div>
                        </div>
                      {/each}
                    </div>
                  {/if}
                </div>
              {/each}
            </div>
          {/if}
        </section>
      {/each}
    </div>
  {/if}
</div>

<style>
  .stacks-view {
    flex: 1;
    overflow-y: auto;
    padding: 16px 24px;
    background: var(--bg-primary);
  }

  .stacks-header {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 16px;
  }

  .stacks-title {
    font-size: 16px;
    font-weight: 600;
    color: var(--text-primary);
    margin: 0;
  }

  .stacks-count {
    font-size: 12px;
    font-weight: 500;
    color: var(--text-muted);
    background: var(--bg-inset);
    padding: 1px 7px;
    border-radius: 10px;
  }

  .stacks-body {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .repo-section {
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    background: var(--bg-surface);
    overflow: hidden;
  }

  .repo-header {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 14px;
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
    border: none;
    background: transparent;
    text-align: left;
    cursor: pointer;
  }

  .repo-header:hover {
    background: var(--bg-surface-hover);
  }

  .repo-name {
    flex: 1;
  }

  .repo-badge {
    font-size: 11px;
    font-weight: 500;
    color: var(--text-muted);
    background: var(--bg-inset);
    padding: 1px 7px;
    border-radius: 10px;
  }

  .chevron {
    flex-shrink: 0;
    transition: transform 0.15s ease;
  }

  .chevron-open {
    transform: rotate(90deg);
  }

  .repo-stacks {
    display: flex;
    flex-direction: column;
    border-top: 1px solid var(--border-muted);
  }

  .stack-card {
    border-bottom: 1px solid var(--border-muted);
  }

  .stack-card:last-child {
    border-bottom: none;
  }

  .stack-header {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 14px 8px 28px;
    font-size: 13px;
    color: var(--text-primary);
    background: transparent;
    text-align: left;
    cursor: pointer;
  }

  .stack-header:hover {
    background: var(--bg-surface-hover);
  }

  .stack-name {
    font-weight: 500;
  }

  .stack-pr-count {
    font-size: 12px;
    color: var(--text-muted);
    margin-right: auto;
  }

  .health-dots {
    display: flex;
    align-items: center;
    gap: 3px;
    margin-right: 4px;
  }

  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    border: 1.5px solid transparent;
    flex-shrink: 0;
  }

  .dot-outline {
    border-width: 1.5px;
    border-style: solid;
  }

  .dot-dimmed {
    opacity: 0.5;
  }

  .health-badge {
    font-size: 11px;
    font-weight: 500;
    padding: 1px 8px;
    border-radius: 10px;
    white-space: nowrap;
  }

  .badge-green {
    background: rgba(35, 134, 54, 0.15);
    color: #238636;
  }

  .badge-yellow {
    background: rgba(210, 153, 34, 0.15);
    color: #d29922;
  }

  .badge-red {
    background: rgba(248, 81, 73, 0.15);
    color: #f85149;
  }

  .stack-members {
    padding: 4px 14px 8px 40px;
  }

  .member-row {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    min-height: 28px;
  }

  .member-dimmed {
    opacity: 0.5;
  }

  .member-chain {
    display: flex;
    flex-direction: column;
    align-items: center;
    width: 10px;
    flex-shrink: 0;
    padding-top: 6px;
  }

  .chain-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    border: 1.5px solid transparent;
    flex-shrink: 0;
  }

  .chain-line {
    width: 1.5px;
    flex: 1;
    min-height: 12px;
    background: var(--border-muted);
    margin-top: 2px;
  }

  .member-info {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: 1;
    min-width: 0;
    padding: 3px 0;
  }

  .member-link {
    font-size: 13px;
    font-weight: 500;
    color: var(--accent-blue);
    cursor: pointer;
    white-space: nowrap;
    background: none;
    border: none;
    padding: 0;
  }

  .member-link:hover {
    text-decoration: underline;
  }

  .member-title {
    font-size: 13px;
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .draft-badge,
  .merged-badge {
    font-size: 11px;
    font-weight: 500;
    padding: 0 6px;
    border-radius: 10px;
    white-space: nowrap;
    flex-shrink: 0;
  }

  .draft-badge {
    background: var(--bg-inset);
    color: var(--text-muted);
  }

  .merged-badge {
    background: rgba(139, 148, 158, 0.15);
    color: #8b949e;
  }

  .empty-state {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    flex: 1;
    color: var(--text-muted);
    font-size: 13px;
    padding: 48px 16px;
  }

  .empty-state.error {
    color: #f85149;
  }

  .loading-spinner {
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
