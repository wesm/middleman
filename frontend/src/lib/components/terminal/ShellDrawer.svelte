<script lang="ts">
  import type { RuntimeSession } from "@middleman/ui/api/types";
  import PanelBottomIcon from "@lucide/svelte/icons/panel-bottom";
  import TerminalPane from "./TerminalPane.svelte";
  import { workspaceShellWebSocketPath } from "../../api/workspace-runtime.js";

  interface ShellDrawerProps {
    workspaceId: string;
    open: boolean;
    loading?: boolean;
    shellSession?: RuntimeSession | null;
    onToggle?: () => void;
    onExit?: () => void;
  }

  const {
    workspaceId,
    open,
    loading = false,
    shellSession = null,
    onToggle,
    onExit,
  }: ShellDrawerProps = $props();
</script>

<div class="shell-drawer">
  <button
    class="drawer-handle"
    aria-label={open ? "Close shell drawer" : "Open shell drawer"}
    onclick={() => onToggle?.()}
  >
    <PanelBottomIcon size="15" strokeWidth="2" aria-hidden="true" />
    <span>Shell</span>
  </button>

  {#if open}
    <div class="drawer-body">
      {#if loading}
        <div class="drawer-state">Starting shell...</div>
      {:else if shellSession}
        <TerminalPane
          websocketPath={workspaceShellWebSocketPath(workspaceId)}
          reconnectOnExit={false}
          onExit={() => onExit?.()}
        />
      {:else}
        <div class="drawer-state">Shell unavailable</div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .shell-drawer {
    flex-shrink: 0;
    border-top: 1px solid var(--border-default);
    background: var(--bg-surface);
  }

  .drawer-handle {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 30px;
    padding: 0 10px;
    border: 0;
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: 12px;
    font-weight: 650;
    cursor: pointer;
  }

  .drawer-handle:hover {
    color: var(--text-primary);
  }

  .drawer-body {
    height: clamp(180px, 32vh, 360px);
    border-top: 1px solid var(--border-muted);
    background: #0d1117;
  }

  .drawer-state {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: var(--text-muted);
    font-size: 13px;
  }
</style>
