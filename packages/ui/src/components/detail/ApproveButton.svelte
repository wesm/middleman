<script lang="ts">
  import CheckIcon from "@lucide/svelte/icons/check";
  import { getClient, getStores } from "../../context.js";
  import ActionButton from "../shared/ActionButton.svelte";
  import { runApprovePR, type PRDetailActionInput } from "./keyboard-actions.js";

  const client = getClient();
  const { detail, pulls } = getStores();

  interface Props {
    owner: string;
    name: string;
    number: number;
    provider: string;
    platformHost?: string | undefined;
    repoPath: string;
    size?: "sm" | "md";
    disabled?: boolean;
  }

  const {
    owner,
    name,
    number,
    provider,
    platformHost,
    repoPath,
    size = "md",
    disabled = false,
  }: Props = $props();

  let expanded = $state(false);
  let body = $state("");
  let submitting = $state(false);
  let error = $state<string | null>(null);

  // Reset draft state on PR identity change so an open form with
  // PR A's body cannot submit to PR B once the route transitions.
  $effect(() => {
    void owner;
    void name;
    void number;
    expanded = false;
    body = "";
    error = null;
  });

  function buildInput(): PRDetailActionInput {
    return {
      pr: { State: "open", IsDraft: false, MergeableState: "" },
      ref: { provider, platformHost, owner, name, repoPath },
      number,
      viewerCan: {
        approve: true, merge: false, markReady: false,
        approveWorkflows: false,
      },
      repoSettings: null,
      stale: disabled,
      stores: { detail, pulls },
      client,
      approveCommentBody: body,
    };
  }

  async function handleApprove(): Promise<void> {
    if (disabled) return;
    submitting = true;
    error = null;
    try {
      await runApprovePR(buildInput());
      body = "";
      expanded = false;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      submitting = false;
    }
  }
</script>

<div class="approve-section">
  {#if expanded}
    <textarea
      class="approve-comment"
      placeholder="Leave an optional comment\u2026"
      bind:value={body}
      rows={3}
    ></textarea>
    {#if error}
      <p class="approve-error">{error}</p>
    {/if}
    <div class="approve-actions">
      <ActionButton
        class="btn btn--secondary"
        onclick={() => { expanded = false; }}
        disabled={submitting}
        tone="neutral"
        surface="outline"
      >
        Cancel
      </ActionButton>
      <ActionButton
        class="btn btn--primary btn--green"
        onclick={() => void handleApprove()}
        disabled={submitting || disabled}
        tone="success"
        surface="solid"
        title="Submit an approving code review on this pull request"
      >
        {submitting ? "Approving\u2026" : "Approve"}
      </ActionButton>
    </div>
  {:else}
    <ActionButton
      class="btn btn--approve"
      onclick={() => { if (!disabled) expanded = true; }}
      {disabled}
      tone="success"
      surface="soft"
      title="Open the approval form to submit a code review on this pull request"
      label="Approve"
      shortLabel="Approve"
      {size}
    >
      <CheckIcon size="14" strokeWidth="2.4" aria-hidden="true" />
    </ActionButton>
  {/if}
</div>

<style>
  .approve-section {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .approve-comment {
    font-size: var(--font-size-root);
    padding: 8px 10px;
    background: var(--bg-inset);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    color: var(--text-primary);
    resize: vertical;
    max-height: 150px;
    line-height: 1.5;
  }
  .approve-comment:focus {
    border-color: var(--accent-blue);
    outline: none;
  }

  .approve-error {
    font-size: var(--font-size-sm);
    color: var(--accent-red);
  }

  .approve-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
  }
</style>
