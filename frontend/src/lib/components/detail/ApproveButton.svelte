<script lang="ts">
  import { approvePR } from "../../api/client.js";
  import { loadDetail } from "../../stores/detail.svelte.js";
  import { loadPulls } from "../../stores/pulls.svelte.js";

  interface Props {
    owner: string;
    name: string;
    number: number;
  }

  const { owner, name, number }: Props = $props();

  let expanded = $state(false);
  let body = $state("");
  let submitting = $state(false);
  let error = $state<string | null>(null);

  async function handleApprove(): Promise<void> {
    submitting = true;
    error = null;
    try {
      await approvePR(owner, name, number, body.trim());
      body = "";
      expanded = false;
      await loadDetail(owner, name, number);
      await loadPulls();
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
      <button
        class="btn btn--secondary"
        onclick={() => { expanded = false; }}
        disabled={submitting}
      >
        Cancel
      </button>
      <button
        class="btn btn--primary btn--green"
        onclick={() => void handleApprove()}
        disabled={submitting}
      >
        {submitting ? "Approving\u2026" : "Approve"}
      </button>
    </div>
  {:else}
    <button
      class="btn btn--approve"
      onclick={() => { expanded = true; }}
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
    </button>
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

  .btn {
    font-size: 13px;
    font-weight: 500;
    padding: 6px 14px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: opacity 0.1s;
  }
  .btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .btn--secondary {
    background: var(--bg-inset);
    color: var(--text-secondary);
    border: 1px solid var(--border-default);
  }
  .btn--secondary:hover:not(:disabled) {
    background: var(--bg-surface-hover);
  }

  .btn--primary {
    color: #fff;
    border: none;
  }

  .btn--green {
    background: #1a7f37;
  }
  .btn--green:hover:not(:disabled) {
    background: #176b2e;
  }

  .btn--approve {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: color-mix(
      in srgb, var(--accent-green) 12%, transparent
    );
    color: var(--accent-green);
    border: 1px solid color-mix(
      in srgb, var(--accent-green) 30%, transparent
    );
    font-weight: 500;
  }
  .btn--approve:hover {
    background: color-mix(
      in srgb, var(--accent-green) 20%, transparent
    );
  }
</style>
