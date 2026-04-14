<script lang="ts">
  import { getClient, getStores } from "../../context.js";
  import ActionButton from "../shared/ActionButton.svelte";

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
  <ActionButton
    class="btn btn--workflow-approval"
    onclick={() => void handleApproveWorkflows()}
    disabled={submitting}
    tone="workflow"
    surface="soft"
  >
    {submitting ? "Approving workflows…" : label}
  </ActionButton>
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
</style>
