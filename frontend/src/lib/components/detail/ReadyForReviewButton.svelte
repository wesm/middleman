<script lang="ts">
  import { markPRReadyForReview } from "../../api/client.js";
  import { loadDetail } from "../../stores/detail.svelte.js";
  import { loadPulls } from "../../stores/pulls.svelte.js";

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
      await markPRReadyForReview(owner, name, number);
      await loadDetail(owner, name, number);
      await loadPulls();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      submitting = false;
    }
  }
</script>

<div class="ready-section">
  <button
    class="btn btn--ready"
    onclick={() => void handleReadyForReview()}
    disabled={submitting}
  >
    {submitting ? "Publishing…" : "Ready for review"}
  </button>
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

  .btn--ready {
    background: color-mix(
      in srgb, var(--accent-blue) 10%, transparent
    );
    color: var(--accent-blue);
    border: 1px solid color-mix(
      in srgb, var(--accent-blue) 30%, transparent
    );
  }

  .btn--ready:hover:not(:disabled) {
    background: color-mix(
      in srgb, var(--accent-blue) 18%, transparent
    );
  }
</style>
