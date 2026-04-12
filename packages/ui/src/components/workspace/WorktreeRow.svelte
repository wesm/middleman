<script lang="ts">
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

  const activityColors: Record<
    WorkspaceActivity["state"],
    string
  > = {
    idle: "var(--text-muted)",
    active: "var(--accent-green)",
    running: "var(--accent-blue)",
    needsAttention: "var(--accent-amber)",
  };

  const prStateColors: Record<
    WorkspaceLinkedPR["state"],
    string
  > = {
    open: "var(--accent-green)",
    closed: "var(--accent-red)",
    merged: "var(--accent-purple)",
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
    class="activity-dot"
    style="background: {activityColors[worktree.activity.state]}"
    title={worktree.activity.state}
  ></span>

  <span class="content">
    <span class="name-row">
      <span class="name">{worktree.name}</span>
      <span class="branch">{worktree.branch}</span>
    </span>

    <span class="meta-row">
      {#if worktree.linkedPR}
        <button
          class="pr-badge"
          style="color: {prStateColors[worktree.linkedPR.state]}"
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
          #{worktree.linkedPR.number}
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
    </span>
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
    height: 38px;
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

  .content {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
    flex: 1;
  }

  .name-row {
    display: flex;
    align-items: baseline;
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

  .branch {
    font-size: 11px;
    font-family: var(--font-mono);
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }

  .meta-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .pr-badge {
    display: flex;
    align-items: center;
    gap: 3px;
    font-size: 11px;
    font-weight: 600;
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
    border-radius: var(--radius-sm, 3px);
    transition: opacity 0.1s;
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
