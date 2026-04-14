<script lang="ts">
  import { getClient, getStores } from "../../context.js";
  import ActionButton from "../shared/ActionButton.svelte";

  const client = getClient();
  const { detail, pulls } = getStores();

  interface Props {
    owner: string;
    name: string;
    number: number;
  }

  const { owner, name, number }: Props = $props();

  let submitting = $state(false);
  let error = $state<string | null>(null);

  async function handleReadyForReview(): Promise<void> {
    submitting = true;
    error = null;
    try {
      const { error } = await client.POST("/repos/{owner}/{name}/pulls/{number}/ready-for-review", {
        params: { path: { owner, name, number } },
      });
      if (error) {
        throw new Error(error.detail ?? error.title ?? "failed to mark pull request ready for review");
      }
      await detail.loadDetail(owner, name, number);
      await pulls.loadPulls();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      submitting = false;
    }
  }
</script>

<div class="ready-section">
  <ActionButton
    class="btn btn--ready"
    onclick={() => void handleReadyForReview()}
    disabled={submitting}
    tone="info"
    surface="soft"
  >
    {submitting ? "Publishing…" : "Ready for review"}
  </ActionButton>
  {#if error}
    <p class="ready-error">{error}</p>
  {/if}
</div>

<style>
  .ready-section {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .ready-error {
    font-size: 12px;
    color: var(--accent-red);
  }
</style>
