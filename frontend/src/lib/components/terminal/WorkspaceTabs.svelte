<script lang="ts">
  import type { RuntimeSession } from "@middleman/ui/api/types";
  import XIcon from "@lucide/svelte/icons/x";
  import HouseIcon from "@lucide/svelte/icons/house";
  import TerminalIcon from "@lucide/svelte/icons/terminal";
  import SparklesIcon from "@lucide/svelte/icons/sparkles";

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

  function sessionStatusClass(status: string): string {
    if (status === "running") return "running";
    if (status === "starting") return "starting";
    return "exited";
  }
</script>

<div class="workspace-tabs" role="tablist" aria-label="Workspace tabs">
  <button
    role="tab"
    aria-selected={activeKey === "home"}
    class={["tab", "tab-home", { active: activeKey === "home" }]}
    onclick={() => onSelectHome?.()}
  >
    <span class="tab-icon" aria-hidden="true">
      <HouseIcon size="13" strokeWidth="2" />
    </span>
    <span class="tab-label">Home</span>
  </button>

  {#if tmuxOpen}
    <div
      class={[
        "tab-with-close",
        "tab",
        "tab-tmux",
        { active: activeKey === "tmux" },
      ]}
    >
      <button
        role="tab"
        aria-selected={activeKey === "tmux"}
        class="tab-button"
        onclick={() => onSelectTmux?.()}
      >
        <span class="tab-icon" aria-hidden="true">
          <TerminalIcon size="13" strokeWidth="2" />
        </span>
        <span class="tab-label">tmux</span>
      </button>
      <button
        class="tab-close"
        aria-label="Close tmux"
        title="Close tab"
        onclick={() => onCloseTmux?.()}
      >
        <XIcon size="12" strokeWidth="2.25" aria-hidden="true" />
      </button>
    </div>
  {/if}

  {#each sessions as session (session.key)}
    <div
      class={[
        "tab-with-close",
        "tab",
        { active: activeKey === `session:${session.key}` },
      ]}
    >
      <button
        role="tab"
        aria-selected={activeKey === `session:${session.key}`}
        class="tab-button"
        onclick={() => onSelectSession?.(session.key)}
      >
        <span class="tab-icon" aria-hidden="true">
          <SparklesIcon size="13" strokeWidth="2" />
        </span>
        <span class="tab-label">{session.label}</span>
        <span
          class={["status-dot", sessionStatusClass(session.status)]}
          title={session.status}
        ></span>
      </button>
      <button
        class="tab-close"
        aria-label={`Close ${session.label}`}
        title="Close tab"
        onclick={() => onCloseSession?.(session.key)}
      >
        <XIcon size="12" strokeWidth="2.25" aria-hidden="true" />
      </button>
    </div>
  {/each}
</div>

<style>
  .workspace-tabs {
    display: flex;
    align-items: stretch;
    gap: 0;
    min-width: 0;
    overflow-x: auto;
    height: 100%;
  }

  .workspace-tabs::-webkit-scrollbar {
    height: 0;
  }

  /* Shared tab chrome — applies to plain buttons and the close-bearing wrapper. */
  .tab {
    position: relative;
    display: inline-flex;
    align-items: center;
    height: 100%;
    border: 0;
    border-right: 1px solid var(--border-muted);
    background: transparent;
    color: var(--text-muted);
    font: inherit;
    font-size: 12px;
    font-weight: 500;
    letter-spacing: 0.005em;
    cursor: pointer;
    flex-shrink: 0;
    transition: background-color 80ms ease, color 80ms ease;
  }

  .tab-home {
    padding: 0 12px;
    gap: 6px;
  }

  .tab:hover:not(.active) {
    background: var(--bg-surface-hover);
    color: var(--text-secondary);
  }

  .tab.active {
    background: var(--bg-surface);
    color: var(--text-primary);
    font-weight: 600;
    /* Pull the active tab down by 1px so its bottom edge meets the
     * editor surface — JetBrains-style "this tab owns the content". */
    margin-bottom: -1px;
    border-bottom: 1px solid var(--bg-surface);
  }

  /* The 2px top accent stripe on the active tab. */
  .tab.active::before {
    content: "";
    position: absolute;
    inset: 0 0 auto 0;
    height: 2px;
    background: var(--accent-blue);
    pointer-events: none;
  }

  .tab-with-close {
    padding: 0 4px 0 10px;
    gap: 4px;
  }

  .tab-button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 100%;
    padding: 0 4px 0 0;
    border: 0;
    background: transparent;
    color: inherit;
    font: inherit;
    cursor: inherit;
  }

  .tab-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--text-muted);
    flex-shrink: 0;
  }

  .tab.active .tab-icon {
    color: var(--accent-blue);
  }

  .tab-label {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 18ch;
  }

  .tab-close {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    border: 0;
    border-radius: 3px;
    background: transparent;
    color: transparent;
    font: inherit;
    cursor: pointer;
    transition: color 80ms ease, background-color 80ms ease;
  }

  .tab-with-close:hover .tab-close,
  .tab-with-close.active .tab-close {
    color: var(--text-muted);
  }

  .tab-close:hover {
    background: var(--bg-inset);
    color: var(--text-primary);
  }

  .status-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent-green);
    box-shadow: 0 0 0 2px var(--bg-surface);
    flex-shrink: 0;
    margin-left: 2px;
  }

  .tab:not(.active) .status-dot {
    box-shadow: none;
  }

  .status-dot.starting {
    background: var(--accent-amber);
    animation: pulse 1.4s ease-in-out infinite;
  }

  .status-dot.exited {
    background: var(--text-muted);
  }

  @keyframes pulse {
    0%, 100% { opacity: 0.5; }
    50% { opacity: 1; }
  }
</style>
