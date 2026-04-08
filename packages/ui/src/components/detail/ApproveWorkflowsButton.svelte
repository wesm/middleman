<script lang="ts">
  import { getClient, getStores } from "../../context.js";

  const client = getClient();
  const { detail, pulls } = getStores();

  interface Props {
    owner: string;
    name: string;
    number: number;
    count: number;
  }

  const { owner, name, number, count }: Props = $props();

  let submitting = $state(false);
  let error = $state<string | null>(null);

  const label = $derived(
    count > 1 ? `Approve workflows (${count})` : "Approve workflows",
  );

  async function handleApproveWorkflows(): Promise<void> {
    submitting = true;
    error = null;
    try {
      const { error: requestError } = await client.POST(
        "/repos/{owner}/{name}/pulls/{number}/approve-workflows",
        {
          params: { path: { owner, name, number } },
        },
      );
      if (requestError) {
        throw new Error(
          requestError.detail ??
            requestError.title ??
            "failed to approve workflows",
        );
      }
      await detail.refreshDetailOnly(owner, name, number);
      await pulls.loadPulls();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      submitting = false;
    }
  }
</script>

<div class="workflow-approval-section">
  <button
    class="btn btn--workflow-approval"
    onclick={() => void handleApproveWorkflows()}
    disabled={submitting}
  >
    {submitting ? "Approving workflows…" : label}
  </button>
  {#if error}
    <p class="workflow-approval-error">{error}</p>
  {/if}
</div>

<style>
  .workflow-approval-section {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .workflow-approval-error {
    font-size: 12px;
    color: var(--accent-red);
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

  .btn--workflow-approval {
    background: color-mix(
      in srgb, var(--accent-purple) 12%, transparent
    );
    color: var(--accent-purple);
    border: 1px solid color-mix(
      in srgb, var(--accent-purple) 30%, transparent
    );
  }

  .btn--workflow-approval:hover:not(:disabled) {
    background: color-mix(
      in srgb, var(--accent-purple) 20%, transparent
    );
  }
</style>
