<script lang="ts">
  import type { EmbedEmptyReason } from "../../stores/router.svelte.ts";

  const { reason }: { reason: EmbedEmptyReason } = $props();

  const message = $derived.by(() => {
    switch (reason) {
      case "noRepo":
        return "Select a repository to see workspaces";
      case "noWorkspace":
        return "No workspace for this item yet";
      case "noSelection":
      default:
        return "Select a workspace from the sidebar";
    }
  });
</script>

<div class="empty">
  <span>{message}</span>
</div>

<style>
  .empty {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    height: 100%;
    color: var(--text-muted);
    font-size: 13px;
    background: var(--bg-primary);
  }
</style>
