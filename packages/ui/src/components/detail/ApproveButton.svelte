<script lang="ts">
  import { getClient, getStores } from "../../context.js";
  import ActionButton from "../shared/ActionButton.svelte";

  const client = getClient();
  const { detail, pulls } = getStores();

  interface Props {
    owner: string;
    name: string;
    number: number;
    size?: "sm" | "md";
  }

  const { owner, name, number, size = "md" }: Props = $props();

  let expanded = $state(false);
  let body = $state("");
  let submitting = $state(false);
  let error = $state<string | null>(null);

  async function handleApprove(): Promise<void> {
    submitting = true;
    error = null;
    try {
      const { error } = await client.POST("/repos/{owner}/{name}/pulls/{number}/approve", {
        params: { path: { owner, name, number } },
        body: { body: body.trim() },
      });
      if (error) {
        throw new Error(error.detail ?? error.title ?? "failed to approve pull request");
      }
      body = "";
      expanded = false;
      await detail.loadDetail(owner, name, number);
      await pulls.loadPulls();
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
        disabled={submitting}
        tone="success"
        surface="solid"
      >
        {submitting ? "Approving\u2026" : "Approve"}
      </ActionButton>
    </div>
  {:else}
    <ActionButton
      class="btn btn--approve"
      onclick={() => { expanded = true; }}
      tone="success"
      surface="soft"
      {size}
    >
      <svg
        width="14"
        height="14"
        viewBox="0 0 16 16"
        fill="currentColor"
      >
        <path d="M13.78 4.22a.75.75 0 010 1.06l-7.25 7.25a.75.75 0 01-1.06 0L2.22 9.28a.75.75 0 011.06-1.06L6 10.94l6.72-6.72a.75.75 0 011.06 0z"/>
      </svg>
      Approve
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
    font-size: 13px;
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
    font-size: 12px;
    color: var(--accent-red);
  }

  .approve-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
  }
</style>
