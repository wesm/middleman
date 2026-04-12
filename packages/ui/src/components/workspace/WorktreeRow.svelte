<script lang="ts">
  import type {
    WorkspaceActivity,
    WorkspaceLinkedPR,
    WorkspaceWorktree,
  } from "../../api/types.js";

  interface Props {
    worktree: WorkspaceWorktree;
    hostKey: string;
    projectKey: string;
    isSelected: boolean;
    onCommand: (
      command: string,
      payload: Record<string, unknown>,
    ) => void;
  }

  let {
    worktree,
    hostKey,
    projectKey,
    isSelected,
    onCommand,
  }: Props = $props();

  let showMenu = $state(false);
  let menuX = $state(0);
  let menuY = $state(0);

  const title = $derived(
    worktree.linkedPR?.title || worktree.name || worktree.branch,
  );
  const showBranch = $derived(
    worktree.branch !== title,
  );

  const activityColors: Record<
    WorkspaceActivity["state"],
    string
  > = {
    idle: "var(--text-muted)",
    active: "var(--accent-green)",
    running: "var(--accent-blue)",
    needsAttention: "var(--accent-amber)",
  };

  function handleContextMenu(e: MouseEvent): void {
    e.preventDefault();
    e.stopPropagation();
    menuX = e.clientX;
    menuY = e.clientY;
    showMenu = true;
  }

  function closeMenu(): void {
    showMenu = false;
  }

  function menuAction(
    command: string,
    payload: Record<string, unknown> = {},
  ): void {
    onCommand(command, {
      hostKey,
      projectKey,
      worktreeKey: worktree.key,
      ...payload,
    });
    closeMenu();
  }
</script>

<svelte:window onclick={showMenu ? closeMenu : undefined} oncontextmenu={showMenu ? closeMenu : undefined} />

<div
  class="worktree-row"
  class:selected={isSelected}
  class:stale={worktree.isStale}
  role="button"
  tabindex="0"
  onclick={() => onCommand("selectWorktree", {
    hostKey,
    projectKey,
    worktreeKey: worktree.key,
  })}
  onkeydown={(e) => { if (e.target === e.currentTarget && (e.key === "Enter" || e.key === " ")) onCommand("selectWorktree", { hostKey, projectKey, worktreeKey: worktree.key }); }}
  oncontextmenu={handleContextMenu}
>
  <span
    class="activity-dot {worktree.activity.state}"
    style="background: {activityColors[worktree.activity.state]}"
    title={worktree.activity.state}
  ></span>

  <span class="content">
    <span class="name-row">
      <span class="name">{title}</span>
      {#if worktree.isPrimary}
        <span class="kind-badge root-badge">ROOT</span>
      {/if}
      {#if worktree.sessionBackend === "localTmux"}
        <span class="kind-badge tmux-badge">tmux</span>
      {/if}
      {#if worktree.isStale}
        <span class="stale-icon" title="Stale worktree">⚠</span>
      {/if}
      <button
        class="delete-btn"
        onclick={(e: MouseEvent) => {
          e.stopPropagation();
          onCommand("requestDeleteWorktree", {
            hostKey,
            projectKey,
            worktreeKey: worktree.key,
          });
        }}
        title="Delete worktree"
      >✕</button>
    </span>

    {#if worktree.linkedPR || worktree.diff || showBranch}
      <span class="meta-row">
        {#if worktree.linkedPR}
          <button
            class="pr-badge pr-{worktree.linkedPR.state}"
            title="Pin PR #{worktree.linkedPR.number}"
            onclick={(e: MouseEvent) => {
              e.stopPropagation();
              onCommand("pinLinkedPR", {
                hostKey,
                projectKey,
                worktreeKey: worktree.key,
                prNumber: worktree.linkedPR!.number,
              });
            }}
          >
            #{worktree.linkedPR.number} {worktree.linkedPR.state.toUpperCase()}
            {#if worktree.linkedPR.checksStatus === "success"}
              <svg
                class="checks-icon"
                width="10"
                height="10"
                viewBox="0 0 16 16"
                fill="var(--accent-green)"
              >
                <path d="M13.78 4.22a.75.75 0 010 1.06l-7.25 7.25a.75.75 0 01-1.06 0L2.22 9.28a.75.75 0 011.06-1.06L6 10.94l6.72-6.72a.75.75 0 011.06 0z"/>
              </svg>
            {:else if worktree.linkedPR.checksStatus === "failure"}
              <svg
                class="checks-icon"
                width="10"
                height="10"
                viewBox="0 0 16 16"
                fill="var(--accent-red)"
              >
                <path d="M3.72 3.72a.75.75 0 011.06 0L8 6.94l3.22-3.22a.75.75 0 111.06 1.06L9.06 8l3.22 3.22a.75.75 0 11-1.06 1.06L8 9.06l-3.22 3.22a.75.75 0 01-1.06-1.06L6.94 8 3.72 4.78a.75.75 0 010-1.06z"/>
              </svg>
            {:else if worktree.linkedPR.checksStatus === "pending"}
              <svg
                class="checks-icon"
                width="10"
                height="10"
                viewBox="0 0 16 16"
              >
                <circle
                  cx="8"
                  cy="8"
                  r="4"
                  fill="var(--accent-amber)"
                />
              </svg>
            {/if}
          </button>
        {/if}

        {#if worktree.diff}
          <span class="diff-summary">
            <span class="diff-added">+{worktree.diff.added}</span>
            <span class="diff-removed">
              -{worktree.diff.removed}
            </span>
          </span>
        {/if}

        {#if showBranch}
          <span class="branch-text">{worktree.branch}</span>
        {/if}
      </span>
    {/if}
  </span>
</div>

{#if showMenu}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="context-menu"
    style="left: {menuX}px; top: {menuY}px"
    oncontextmenu={(e) => e.preventDefault()}
  >
    <button
      class="menu-item"
      onclick={() => menuAction("openWorktreeSession")}
    >
      Open session
    </button>
    {#if worktree.linkedPR}
      <button
        class="menu-item"
        onclick={() => menuAction("pinLinkedPR", {
          prNumber: worktree.linkedPR?.number,
        })}
      >
        Pin linked PR
      </button>
    {/if}
    <button
      class="menu-item menu-item--danger"
      onclick={() => menuAction("deleteWorktree")}
    >
      Delete worktree
    </button>
    {#if worktree.isStale}
      <button
        class="menu-item menu-item--danger"
        onclick={() => menuAction("removeStaleWorktree")}
      >
        Remove stale worktree
      </button>
    {/if}
    <div class="menu-separator"></div>
    <button
      class="menu-item"
      onclick={() => menuAction("setWorktreeHidden", {
        hidden: !worktree.isHidden,
      })}
    >
      {worktree.isHidden ? "Show" : "Hide"} worktree
    </button>
    <button
      class="menu-item"
      onclick={() => menuAction("setSessionBackend")}
    >
      Set session backend
    </button>
  </div>
{/if}

<style>
  .worktree-row {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    min-height: 38px;
    padding: 0 12px;
    text-align: left;
    background: var(--bg-surface);
    border: none;
    border-left: 3px solid transparent;
    cursor: pointer;
    transition: background 0.1s;
  }

  .worktree-row:hover {
    background: var(--bg-surface-hover);
  }

  .worktree-row.selected {
    background: var(--bg-inset);
    border-left-color: var(--accent-blue);
  }

  .worktree-row.stale {
    opacity: 0.55;
  }

  .activity-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }

  @keyframes pulse {
    0%, 100% { opacity: 1; transform: scale(1); }
    50% { opacity: 0.5; transform: scale(1.3); }
  }

  .activity-dot.running,
  .activity-dot.needsAttention {
    animation: pulse 1.5s ease-in-out infinite;
  }

  .content {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
    flex: 1;
  }

  .name-row {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }

  .name {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .kind-badge {
    font-size: 9px;
    font-weight: 600;
    text-transform: uppercase;
    padding: 1px 5px;
    border-radius: 3px;
    flex-shrink: 0;
  }

  .root-badge {
    background: color-mix(
      in srgb, var(--accent-blue) 15%, transparent
    );
    color: var(--accent-blue);
  }

  .tmux-badge {
    background: color-mix(
      in srgb, var(--accent-amber) 15%, transparent
    );
    color: var(--accent-amber);
  }

  .stale-icon {
    color: var(--accent-amber);
    font-size: 12px;
    flex-shrink: 0;
  }

  .delete-btn {
    display: none;
    background: none;
    border: none;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 12px;
    padding: 2px 4px;
    border-radius: 3px;
    flex-shrink: 0;
    margin-left: auto;
  }

  .worktree-row:hover .delete-btn {
    display: inline-flex;
  }

  .delete-btn:hover {
    color: var(--accent-red);
    background: color-mix(
      in srgb, var(--accent-red) 12%, transparent
    );
  }

  .meta-row {
    display: flex;
    align-items: center;
    gap: 6px;
    padding-left: 0;
    font-size: 11px;
  }

  .pr-badge {
    display: flex;
    align-items: center;
    gap: 3px;
    font-size: 10px;
    font-weight: 600;
    background: none;
    border: none;
    padding: 1px 6px;
    cursor: pointer;
    border-radius: 3px;
    transition: opacity 0.1s;
  }

  .pr-open {
    color: var(--accent-green);
    background: color-mix(
      in srgb, var(--accent-green) 12%, transparent
    );
  }

  .pr-merged {
    color: var(--accent-purple);
    background: color-mix(
      in srgb, var(--accent-purple) 12%, transparent
    );
  }

  .pr-closed {
    color: var(--accent-red);
    background: color-mix(
      in srgb, var(--accent-red) 12%, transparent
    );
  }

  .pr-draft {
    color: var(--text-muted);
    background: color-mix(
      in srgb, var(--text-muted) 12%, transparent
    );
  }

  .pr-badge:hover {
    opacity: 0.75;
  }

  .checks-icon {
    flex-shrink: 0;
  }

  .diff-summary {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 11px;
    font-family: var(--font-mono);
  }

  .diff-added {
    color: var(--accent-green);
  }

  .diff-removed {
    color: var(--accent-red);
  }

  .branch-text {
    font-family: var(--font-mono);
    color: var(--text-muted);
    font-size: 11px;
  }

  .context-menu {
    position: fixed;
    z-index: 1000;
    min-width: 180px;
    background: var(--bg-surface);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    padding: 4px 0;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
  }

  .menu-item {
    display: block;
    width: 100%;
    padding: 6px 12px;
    text-align: left;
    font-size: 12px;
    color: var(--text-primary);
    background: none;
    border: none;
    cursor: pointer;
  }

  .menu-item:hover {
    background: var(--bg-surface-hover);
  }

  .menu-item--danger {
    color: var(--accent-red);
  }

  .menu-separator {
    height: 1px;
    margin: 4px 0;
    background: var(--border-muted);
  }
</style>
