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
    disabled?: boolean;
  }

  const {
    owner,
    name,
    number,
    size = "md",
    disabled = false,
  }: Props = $props();

  let submitting = $state(false);
  let error = $state<string | null>(null);

  function shouldRefreshStaleDraftState(message: string): boolean {
    return message.includes("ready for review") && message.includes("404 Not Found");
  }

  async function handleReadyForReview(): Promise<void> {
    if (disabled) return;
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
      const message = err instanceof Error ? err.message : String(err);
      if (shouldRefreshStaleDraftState(message)) {
        try {
          await detail.loadDetail(owner, name, number);
          await pulls.loadPulls();
        } catch {
          // Preserve the original mutation error if the stale-state refresh also fails.
        }
      }
      error = message;
    } finally {
      submitting = false;
    }
  }
</script>

<div class="ready-section">
  <ActionButton
    class="btn btn--ready"
    onclick={() => void handleReadyForReview()}
    disabled={submitting || disabled}
    tone="info"
    surface="soft"
    {size}
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
