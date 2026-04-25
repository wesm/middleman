<script lang="ts">
  import type { LaunchTarget } from "@middleman/ui/api/types";
  import PlusIcon from "@lucide/svelte/icons/plus";

  interface LaunchMenuProps {
    launchTargets: LaunchTarget[];
    launchingKey?: string | null;
    onLaunch?: (targetKey: string) => void;
  }

  const {
    launchTargets,
    launchingKey = null,
    onLaunch,
  }: LaunchMenuProps = $props();

  let open = $state(false);

  const visibleTargets = $derived(
    launchTargets.filter((target) => target.kind !== "plain_shell"),
  );

  function launch(targetKey: string): void {
    open = false;
    onLaunch?.(targetKey);
  }
</script>

<div class="launch-menu">
  <button
    class="launch-trigger"
    aria-label="Launch"
    aria-expanded={open}
    onclick={() => {
      open = !open;
    }}
  >
    <PlusIcon size="15" strokeWidth="2" aria-hidden="true" />
    <span>Launch</span>
  </button>
  {#if open}
    <div class="launch-popover">
      {#each visibleTargets as target (target.key)}
        <button
          class="launch-option"
          disabled={!target.available || launchingKey === target.key}
          title={target.disabled_reason ?? target.label}
          onclick={() => launch(target.key)}
        >
          <span>{target.label}</span>
          <small>{target.kind === "tmux" ? "tmux" : target.source}</small>
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .launch-menu {
    position: relative;
  }

  .launch-trigger {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    height: 28px;
    padding: 0 9px;
    border: 1px solid var(--border-default);
    border-radius: 6px;
    background: var(--bg-surface);
    color: var(--text-secondary);
    font: inherit;
    font-size: 12px;
    font-weight: 600;
    cursor: pointer;
  }

  .launch-trigger:hover {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  .launch-popover {
    position: absolute;
    right: 0;
    top: calc(100% + 6px);
    z-index: 20;
    min-width: 190px;
    padding: 5px;
    border: 1px solid var(--border-default);
    border-radius: 6px;
    background: var(--bg-surface);
    box-shadow: var(--shadow-lg);
  }

  .launch-option {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    width: 100%;
    min-height: 30px;
    padding: 5px 7px;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: 12px;
    text-align: left;
    cursor: pointer;
  }

  .launch-option:hover:not(:disabled) {
    background: var(--bg-surface-hover);
  }

  .launch-option:disabled {
    cursor: not-allowed;
    color: var(--text-muted);
    opacity: 0.7;
  }

  .launch-option small {
    color: var(--text-muted);
    font-size: 11px;
  }
</style>
