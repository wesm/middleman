<script lang="ts">
  import SendHorizontalIcon from "@lucide/svelte/icons/send-horizontal";
  import { getClient, getStores } from "../../context.js";
  import ActionButton from "../shared/ActionButton.svelte";

  const client = getClient();
  const { detail, pulls } = getStores();

  interface Props {
    owner: string;
    name: string;
    number: number;
    platformHost?: string | undefined;
    size?: "sm" | "md";
    disabled?: boolean;
    oncompleted?: () => void;
  }

  const {
    owner,
    name,
    number,
    platformHost,
    size = "md",
    disabled = false,
    oncompleted,
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
        params: {
          path: { owner, name, number },
        },
        body: { platform_host: platformHost ?? "" },
      });
      if (error) {
        throw new Error(error.detail ?? error.title ?? "failed to mark pull request ready for review");
      }
      await detail.loadDetail(owner, name, number, { platformHost });
      await pulls.loadPulls();
      oncompleted?.();
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      if (shouldRefreshStaleDraftState(message)) {
        try {
          await detail.loadDetail(owner, name, number, { platformHost });
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
    label={submitting ? "Publishing…" : "Ready for review"}
    shortLabel={submitting ? "Publishing…" : "Ready"}
    {size}
  >
    <SendHorizontalIcon size="14" strokeWidth="2.2" aria-hidden="true" />
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
