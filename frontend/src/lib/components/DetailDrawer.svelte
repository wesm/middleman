<script lang="ts">
  import PullDetail from "./detail/PullDetail.svelte";
  import IssueDetail from "./detail/IssueDetail.svelte";

  interface Props {
    itemType: "pr" | "issue";
    owner: string;
    name: string;
    number: number;
    onClose: () => void;
  }

  let { itemType, owner, name, number, onClose }: Props = $props();

  function handleBackdropClick(e: MouseEvent): void {
    if (e.target === e.currentTarget) {
      onClose();
    }
  }

  function handleKeydown(e: KeyboardEvent): void {
    if (e.key === "Escape" && !e.defaultPrevented) {
      e.preventDefault();
      onClose();
    }
  }

  $effect(() => {
    window.addEventListener("keydown", handleKeydown);
    return () => window.removeEventListener("keydown", handleKeydown);
  });
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="drawer-backdrop" onclick={handleBackdropClick}>
  <aside class="drawer-panel">
    <div class="drawer-header">
      <button class="close-btn" onclick={onClose} title="Close (Esc)">&#x2715;</button>
      <span class="drawer-title">
        {owner}/{name}#{number}
      </span>
    </div>
    <div class="drawer-body">
      {#key `${owner}/${name}/${number}`}
        {#if itemType === "pr"}
          <PullDetail {owner} {name} {number} />
        {:else}
          <IssueDetail {owner} {name} {number} />
        {/if}
      {/key}
    </div>
  </aside>
</div>

<style>
  .drawer-backdrop {
    position: fixed;
    top: var(--header-height);
    left: 0;
    right: 0;
    bottom: var(--status-bar-height);
    z-index: 100;
  }

  .drawer-panel {
    position: absolute;
    top: 0;
    right: 0;
    bottom: 0;
    width: 65%;
    min-width: 500px;
    background: var(--bg-surface);
    border-left: 1px solid var(--border-default);
    box-shadow: var(--shadow-lg);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .drawer-header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    border-bottom: 1px solid var(--border-default);
    flex-shrink: 0;
  }

  .close-btn {
    padding: 4px 8px;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    font-size: 14px;
  }

  .close-btn:hover {
    color: var(--text-primary);
    background: var(--bg-surface-hover);
  }

  .drawer-title {
    font-size: 12px;
    color: var(--text-muted);
  }

  .drawer-body {
    flex: 1;
    overflow-y: auto;
  }

  :global(#app.container-narrow) .drawer-panel,
  :global(#app.container-medium) .drawer-panel {
    width: 100%;
    min-width: 0;
  }
</style>
