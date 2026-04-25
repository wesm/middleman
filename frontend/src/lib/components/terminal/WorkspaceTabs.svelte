<script lang="ts">
  import type { RuntimeSession } from "@middleman/ui/api/types";
  import XIcon from "@lucide/svelte/icons/x";

  interface WorkspaceTabsProps {
    activeKey: string;
    sessions: RuntimeSession[];
    tmuxOpen?: boolean;
    onSelectHome?: () => void;
    onSelectTmux?: () => void;
    onSelectSession?: (sessionKey: string) => void;
    onCloseTmux?: () => void;
    onCloseSession?: (sessionKey: string) => void;
  }

  const {
    activeKey,
    sessions,
    tmuxOpen = false,
    onSelectHome,
    onSelectTmux,
    onSelectSession,
    onCloseTmux,
    onCloseSession,
  }: WorkspaceTabsProps = $props();
</script>

<div class="workspace-tabs" role="tablist" aria-label="Workspace tabs">
  <button
    role="tab"
    class={["tab", { active: activeKey === "home" }]}
    onclick={() => onSelectHome?.()}
  >
    Home
  </button>

  {#if tmuxOpen}
    <div class={["tab-with-close", { active: activeKey === "tmux" }]}>
      <button
        role="tab"
        class="tab inner"
        onclick={() => onSelectTmux?.()}
      >
        tmux
      </button>
      <button
        class="tab-close"
        aria-label="Close tmux"
        onclick={() => onCloseTmux?.()}
      >
        <XIcon size="13" strokeWidth="2" aria-hidden="true" />
      </button>
    </div>
  {/if}

  {#each sessions as session (session.key)}
    <div
      class={[
        "tab-with-close",
        { active: activeKey === `session:${session.key}` },
      ]}
    >
      <button
        role="tab"
        class="tab inner"
        onclick={() => onSelectSession?.(session.key)}
      >
        {session.label}
        <span
          class={[
            "status-dot",
            { exited: session.status !== "running" },
          ]}
        ></span>
      </button>
      <button
        class="tab-close"
        aria-label={`Close ${session.label}`}
        onclick={() => onCloseSession?.(session.key)}
      >
        <XIcon size="13" strokeWidth="2" aria-hidden="true" />
      </button>
    </div>
  {/each}
</div>

<style>
  .workspace-tabs {
    display: flex;
    align-items: center;
    gap: 3px;
    min-width: 0;
    overflow-x: auto;
  }

  .tab,
  .tab-close {
    border: 0;
    background: transparent;
    color: var(--text-muted);
    font: inherit;
    cursor: pointer;
  }

  .tab {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    height: 28px;
    padding: 0 9px;
    border-radius: 5px;
    white-space: nowrap;
    font-size: 12px;
    font-weight: 600;
  }

  .tab:hover,
  .tab-with-close:hover {
    background: var(--bg-surface-hover);
    color: var(--text-secondary);
  }

  .tab.active,
  .tab-with-close.active {
    background: var(--bg-inset);
    color: var(--text-primary);
  }

  .tab-with-close {
    display: inline-flex;
    align-items: center;
    height: 28px;
    border-radius: 5px;
  }

  .tab.inner {
    padding-right: 5px;
  }

  .tab-close {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 24px;
    margin-right: 2px;
    border-radius: 4px;
  }

  .tab-close:hover {
    background: var(--bg-surface);
    color: var(--text-primary);
  }

  .status-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent-green);
  }

  .status-dot.exited {
    background: var(--text-muted);
  }
</style>
