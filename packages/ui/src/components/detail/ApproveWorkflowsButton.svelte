<script lang="ts">
  import WorkflowIcon from "@lucide/svelte/icons/workflow";
  import { getClient, getStores } from "../../context.js";
  import ActionButton from "../shared/ActionButton.svelte";

  const client = getClient();
  const { detail, pulls } = getStores();

  interface Props {
    owner: string;
    name: string;
    number: number;
    platformHost?: string | undefined;
    count: number;
    size?: "sm" | "md";
    disabled?: boolean;
    oncompleted?: () => void;
  }

  const {
    owner,
    name,
    number,
    platformHost,
    count,
    size = "md",
    disabled = false,
    oncompleted,
  }: Props = $props();

  let submitting = $state(false);
  let error = $state<string | null>(null);

  const label = $derived(
    count > 1 ? `Approve workflows (${count})` : "Approve workflows",
  );
  const shortLabel = $derived(
    count > 1 ? `Workflows (${count})` : "Workflows",
  );
  const tooltip =
    "Approve pending GitHub Actions runs waiting on outside contributor approval";

  async function handleApproveWorkflows(): Promise<void> {
    if (disabled) return;
    submitting = true;
    error = null;
    try {
      const { error: requestError } = await client.POST(
        "/repos/{owner}/{name}/pulls/{number}/approve-workflows",
        {
          params: {
            path: { owner, name, number },
          },
          body: { platform_host: platformHost ?? "" },
        },
      );
      if (requestError) {
        throw new Error(
          requestError.detail ??
            requestError.title ??
            "failed to approve workflows",
        );
      }
      await detail.refreshDetailOnly(owner, name, number, platformHost);
      await pulls.loadPulls();
      oncompleted?.();
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
    disabled={submitting || disabled}
    tone="workflow"
    surface="soft"
    title={tooltip}
    label={submitting ? "Approving workflows…" : label}
    shortLabel={submitting ? "Approving…" : shortLabel}
    {size}
  >
    <WorkflowIcon size="14" strokeWidth="2.2" aria-hidden="true" />
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
