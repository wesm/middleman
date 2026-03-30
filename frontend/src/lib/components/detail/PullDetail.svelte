<script lang="ts">
  import {
    getDetail,
    isDetailLoading,
    getDetailError,
    loadDetail,
    updateKanbanState,
  } from "../../stores/detail.svelte.js";
  import type { KanbanStatus } from "../../api/types.js";
  import EventTimeline from "./EventTimeline.svelte";
  import CommentBox from "./CommentBox.svelte";

  interface Props {
    owner: string;
    name: string;
    number: number;
  }

  const { owner, name, number }: Props = $props();

  $effect(() => {
    void loadDetail(owner, name, number);
  });

  const kanbanOptions: { value: KanbanStatus; label: string }[] = [
    { value: "new", label: "New" },
    { value: "reviewing", label: "Reviewing" },
    { value: "waiting", label: "Waiting" },
    { value: "awaiting_merge", label: "Awaiting Merge" },
  ];

  function timeAgo(dateStr: string): string {
    const diffMs = Date.now() - new Date(dateStr).getTime();
    const diffMin = Math.floor(diffMs / 60_000);
    if (diffMin < 60) return `${diffMin}m ago`;
    const diffHr = Math.floor(diffMin / 60);
    if (diffHr < 24) return `${diffHr}h ago`;
    return `${Math.floor(diffHr / 24)}d ago`;
  }

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
      </div>

      <!-- Chips row -->
      <div class="chips-row">
        {#if pr.IsDraft}
          <span class="chip chip--amber">Draft</span>
        {/if}
        {#if pr.CIStatus}
          <span class="chip {ciColor(pr.CIStatus)}">CI: {pr.CIStatus}</span>
        {/if}
        {#if pr.ReviewDecision}
          <span class="chip {reviewColor(pr.ReviewDecision)}">{pr.ReviewDecision.replace(/_/g, " ")}</span>
        {/if}
        <span class="chip chip--green">+{pr.Additions}</span>
        <span class="chip chip--red">-{pr.Deletions}</span>
      </div>

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

      <!-- PR body -->
      {#if pr.Body}
        <div class="section">
          <div class="inset-box">{pr.Body}</div>
        </div>
      {/if}

      <!-- Activity -->
      <div class="section">
        <h3 class="section-title">Activity</h3>
        <EventTimeline events={detail.events} />
      </div>

      <!-- Comment box -->
      <div class="section">
        <CommentBox {owner} {name} {number} />
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

  .chip--muted {
    background: var(--bg-inset);
    color: var(--text-muted);
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

  .kanban-select--new { color: var(--accent-blue); }
  .kanban-select--reviewing { color: var(--accent-amber); }
  .kanban-select--waiting { color: var(--accent-purple); }
  .kanban-select--awaiting-merge { color: var(--accent-green); }

  .section {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .section-title {
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-muted);
  }

  .inset-box {
    font-size: 13px;
    color: var(--text-primary);
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-sm);
    padding: 10px 12px;
    white-space: pre-wrap;
    word-break: break-word;
    line-height: 1.6;
  }
</style>
