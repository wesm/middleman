<script lang="ts">
  import { getClient, getNavigate } from "../../context.js";

  const client = getClient();
  const navigate = getNavigate();

  interface Props {
    owner: string;
    name: string;
    number: number;
  }

  const { owner, name, number }: Props = $props();

  interface StackMember {
    number: number;
    title: string;
    state: string;
    ci_status: string;
    review_decision: string;
    position: number;
    is_draft: boolean;
    blocked_by: number | null;
  }

  interface StackContext {
    stack_id: number;
    stack_name: string;
    position: number;
    size: number;
    health: string;
    members: StackMember[] | null;
  }

  let data = $state<StackContext | null>(null);
  let visible = $state(false);
  let requestSeq = 0;

  $effect(() => {
    // Track props to refetch on change
    const o = owner;
    const n = name;
    const num = number;
    const seq = ++requestSeq;

    visible = false;
    data = null;

    client.GET("/repos/{owner}/{name}/pulls/{number}/stack", {
      params: { path: { owner: o, name: n, number: num } },
    }).then(({ data: resp, error }) => {
      if (seq !== requestSeq) return;
      if (error || !resp) {
        visible = false;
        return;
      }
      data = resp as StackContext;
      visible = true;
    }).catch(() => {
      if (seq !== requestSeq) return;
      visible = false;
    });
  });

  function getDotColor(member: StackMember): { color: string; outline: boolean } {
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
    return { color: "#21262d", outline: true };
  }
</script>

{#if visible && data && data.members}
  <aside class="stack-sidebar">
    <div class="stack-header">STACK &middot; {data.stack_name}</div>

    <div class="stack-chain">
      {#each data.members as member, i}
        {@const dot = getDotColor(member)}
        {@const isCurrent = member.number === number}
        <div
          class="chain-item"
          class:chain-item--dimmed={member.blocked_by != null}
        >
          <div class="chain-visual">
            <span
              class="chain-dot"
              class:chain-dot--current={isCurrent}
              class:chain-dot--outline={dot.outline}
              style:background={isCurrent ? "var(--accent-purple)" : dot.outline ? "transparent" : dot.color}
              style:border-color={isCurrent ? "var(--accent-purple)" : dot.outline ? dot.color : "transparent"}
              style:width={isCurrent ? "10px" : "8px"}
              style:height={isCurrent ? "10px" : "8px"}
            ></span>
            {#if i < data.members.length - 1}
              <span class="chain-line"></span>
            {/if}
          </div>
          <div class="chain-content">
            <div class="member-row">
              <button
                class="member-link"
                onclick={() => navigate(`/pulls/${owner}/${name}/${member.number}`)}
              >
                #{member.number}
              </button>
              <span class="member-title">{member.title}</span>
            </div>
            <div class="member-meta">
              {#if member.ci_status}
                <span
                  class="status-dot"
                  class:status-dot--outline={dot.outline}
                  style:background={dot.outline ? "transparent" : dot.color}
                  style:border-color={dot.outline ? dot.color : "transparent"}
                ></span>
              {/if}
              {#if member.review_decision}
                <span class="review-label">{member.review_decision.replace(/_/g, " ").toLowerCase()}</span>
              {/if}
            </div>
            {#if isCurrent}
              <span class="you-are-here">You are here</span>
            {/if}
            {#if member.blocked_by != null}
              <span class="blocked-label">blocked by #{member.blocked_by}</span>
            {/if}
          </div>
        </div>
      {/each}
    </div>

    <button
      class="view-full-link"
      onclick={() => navigate("/stacks")}
    >
      View full stack
    </button>
  </aside>
{/if}

<style>
  .stack-sidebar {
    width: 200px;
    flex-shrink: 0;
    border-left: 1px solid var(--border-default);
    background: var(--bg-surface);
    padding: 14px 12px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .stack-header {
    color: var(--accent-purple);
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .stack-chain {
    display: flex;
    flex-direction: column;
  }

  .chain-item {
    display: flex;
    gap: 8px;
  }

  .chain-item--dimmed {
    opacity: 0.5;
  }

  .chain-visual {
    display: flex;
    flex-direction: column;
    align-items: center;
    width: 12px;
    flex-shrink: 0;
    padding-top: 3px;
  }

  .chain-dot {
    border-radius: 50%;
    border: 1.5px solid transparent;
    flex-shrink: 0;
  }

  .chain-dot--outline {
    border-width: 1.5px;
    border-style: solid;
  }

  .chain-dot--current {
    box-shadow: 0 0 0 2px rgba(163, 113, 247, 0.3);
  }

  .chain-line {
    width: 2px;
    flex: 1;
    min-height: 10px;
    background: var(--border-default);
    margin: 2px 0;
  }

  .chain-content {
    flex: 1;
    min-width: 0;
    padding-bottom: 10px;
  }

  .member-row {
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .member-link {
    font-size: 12px;
    font-weight: 500;
    color: var(--accent-blue);
    cursor: pointer;
    background: none;
    border: none;
    padding: 0;
    text-align: left;
  }

  .member-link:hover {
    text-decoration: underline;
  }

  .member-title {
    font-size: 11px;
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .member-meta {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-top: 2px;
  }

  .status-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    border: 1px solid transparent;
    flex-shrink: 0;
  }

  .status-dot--outline {
    border-style: solid;
  }

  .review-label {
    font-size: 10px;
    color: var(--text-muted);
    text-transform: lowercase;
  }

  .you-are-here {
    display: block;
    font-size: 10px;
    color: var(--accent-purple);
    font-weight: 500;
    margin-top: 2px;
  }

  .blocked-label {
    display: block;
    font-size: 10px;
    color: #f85149;
    font-style: italic;
    margin-top: 2px;
  }

  .view-full-link {
    font-size: 11px;
    color: var(--accent-blue);
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
    text-align: left;
    margin-top: auto;
  }

  .view-full-link:hover {
    text-decoration: underline;
  }
</style>
