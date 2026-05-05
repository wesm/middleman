<script lang="ts">
  import { onDestroy } from "svelte";
  import { providerItemPath, providerRouteParams } from "../../api/provider-routes.js";
  import { getClient, getNavigate, getStores } from "../../context.js";
  import {
    buildPullRequestRoute,
    type PullRequestRouteRef,
  } from "../../routes.js";

  const client = getClient();
  const navigate = getNavigate();
  const { sync } = getStores();

  type Props = PullRequestRouteRef;

  const { owner, name, number, provider, platformHost, repoPath }: Props = $props();

  interface StackMember {
    number: number;
    title: string;
    state: string;
    ci_status: string;
    review_decision: string;
    position: number;
    is_draft: boolean;
    base_branch: string;
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

  function fetchStack(o: string, n: string, num: number): void {
    const ref = { provider, platformHost, owner: o, name: n, repoPath };
    const seq = ++requestSeq;
    client.GET(providerItemPath("pulls", ref, "/stack"), {
      params: { path: { ...providerRouteParams(ref), number: num } },
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
  }

  $effect(() => {
    const o = owner;
    const n = name;
    const num = number;
    visible = false;
    data = null;
    fetchStack(o, n, num);
  });

  // Refetch stack state when a sync completes — PR states (CI, review,
  // merge status) can change for the same PR number while the sidebar is
  // open and must be reflected without navigation.
  const unsubSync = sync.subscribeSyncComplete(() => fetchStack(owner, name, number));
  onDestroy(() => unsubSync());

  // Shared "merge-ready" predicate used for dot color, outline, and
  // the ready-to-merge label. Drafts never count as merge-ready even
  // with green CI + approval because GitHub will not merge a draft PR.
  function isMergeReady(member: StackMember): boolean {
    return member.state === "open"
      && !member.is_draft
      && member.ci_status === "success"
      && member.review_decision === "APPROVED";
  }

  function getDotColor(member: StackMember): string {
    if (member.state === "merged") return "#8b949e";
    if (member.ci_status === "failure") return "#f85149";
    if (member.ci_status === "pending" || member.review_decision === "CHANGES_REQUESTED") return "#d29922";
    if (isMergeReady(member)) return "#238636";
    return "#21262d";
  }

  function isOutline(member: StackMember): boolean {
    return member.state !== "merged"
      && member.ci_status !== "failure"
      && !(member.ci_status === "pending" || member.review_decision === "CHANGES_REQUESTED")
      && !isMergeReady(member);
  }

  function ciLabel(member: StackMember): { text: string; color: string } | null {
    if (!member.ci_status || member.state === "merged") return null;
    if (member.ci_status === "success") return { text: "\u2713 CI", color: "#238636" };
    if (member.ci_status === "failure") return { text: "\u2717 CI", color: "#f85149" };
    if (member.ci_status === "pending") return { text: "\u25CB CI", color: "#8b949e" };
    return null;
  }

  function reviewLabel(member: StackMember): { text: string; color: string } | null {
    if (!member.review_decision || member.state === "merged") return null;
    if (member.review_decision === "APPROVED") return { text: "\u2713 Approved", color: "#238636" };
    if (member.review_decision === "CHANGES_REQUESTED") return { text: "\u2717 Changes", color: "#f85149" };
    return { text: "\u25CB Review", color: "#8b949e" };
  }

  function isBaseReady(member: StackMember, idx: number): boolean {
    return idx === 0 && isMergeReady(member);
  }
</script>

{#if visible && data && data.members}
  <aside class="stack-sidebar">
    <div class="stack-header">STACK &middot; {data.stack_name}</div>

    <div class="stack-chain">
      {#each data.members as member, i (member.number)}
        {@const isCurrent = member.number === number}
        {@const outline = isOutline(member)}
        {@const ci = ciLabel(member)}
        {@const review = reviewLabel(member)}
        {@const isLast = i === data.members.length - 1}
        <div class="chain-row">
          <div class="chain-rail">
            <span
              class="chain-dot"
              style:background={isCurrent ? "var(--accent-purple)" : outline ? "transparent" : getDotColor(member)}
              style:border-color={isCurrent ? "var(--accent-purple)" : outline ? "#30363d" : "transparent"}
              style:width={isCurrent ? "10px" : "8px"}
              style:height={isCurrent ? "10px" : "8px"}
            ></span>
            {#if !isLast}
              <span class="chain-line"></span>
            {/if}
          </div>
          <div
            class="chain-member"
            class:chain-member--current={isCurrent}
            class:chain-member--dimmed={member.blocked_by != null && !isCurrent}
          >
            {#if isCurrent}
              <div class="current-label">You are here</div>
            {/if}
            <button
              class="member-link"
              onclick={() => navigate(buildPullRequestRoute({ owner, name, number: member.number }))}
            >
              #{member.number} {member.title}
            </button>
            {#if ci || review}
              <div class="member-badges">
                {#if ci}<span style:color={ci.color}>{ci.text}</span>{/if}
                {#if review}<span style:color={review.color}>{review.text}</span>{/if}
              </div>
            {/if}
            {#if isBaseReady(member, i)}
              <div class="ready-label">Ready to merge &rarr; {member.base_branch || "base"}</div>
            {/if}
            {#if member.blocked_by != null}
              <div class="blocked-label">blocked by #{member.blocked_by}</div>
            {/if}
          </div>
        </div>
      {/each}
    </div>

  </aside>
{/if}

<style>
  .stack-sidebar {
    width: 200px;
    flex-shrink: 0;
    border-left: 1px solid var(--border-default);
    background: var(--bg-primary);
    padding: 16px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
  }

  .stack-header {
    color: var(--accent-purple);
    font-weight: 600;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin-bottom: 12px;
  }

  .stack-chain {
    display: flex;
    flex-direction: column;
  }

  .chain-row {
    display: flex;
  }

  .chain-rail {
    display: flex;
    flex-direction: column;
    align-items: center;
    width: 14px;
    flex-shrink: 0;
    margin-right: 10px;
    padding-top: 4px;
  }

  .chain-dot {
    border-radius: 50%;
    border: 1px solid transparent;
    flex-shrink: 0;
  }

  .chain-line {
    width: 2px;
    flex: 1;
    min-height: 8px;
    background: var(--border-default);
    margin-top: 2px;
  }

  .chain-member {
    flex: 1;
    min-width: 0;
    font-size: 12px;
    padding-bottom: 12px;
  }

  .chain-member--current {
    padding: 4px 8px;
    margin-bottom: 0;
    background: color-mix(in srgb, var(--accent-purple) 13%, transparent);
    border-left: 2px solid var(--accent-purple);
    border-radius: 0 4px 4px 0;
  }

  .chain-member--dimmed {
    opacity: 0.5;
  }

  .current-label {
    color: var(--text-primary);
    font-weight: 600;
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.3px;
    margin-bottom: 1px;
  }

  .member-link {
    color: var(--accent-blue);
    cursor: pointer;
    background: none;
    border: none;
    padding: 0;
    font-size: 12px;
    text-align: left;
    display: block;
  }

  .member-link:hover {
    text-decoration: underline;
  }

  .member-badges {
    display: flex;
    gap: 4px;
    margin-top: 2px;
    font-size: 10px;
  }

  .ready-label {
    color: #238636;
    font-size: 10px;
    margin-top: 2px;
  }

  .blocked-label {
    color: #f85149;
    font-size: 10px;
    font-style: italic;
    margin-top: 2px;
  }

</style>
