<script lang="ts">
  import type {
    LaunchTarget,
    RuntimeSession,
  } from "@middleman/ui/api/types";

  interface WorkspaceHomeWorkspace {
    id: string;
    repo_owner: string;
    repo_name: string;
    item_number: number;
    git_head_ref: string;
    worktree_path: string;
    mr_title?: string | null;
  }

  interface WorkspaceHomeProps {
    workspace: WorkspaceHomeWorkspace;
    launchTargets: LaunchTarget[];
    sessions: RuntimeSession[];
    launchingKey?: string | null;
    onLaunch?: (targetKey: string) => void;
    onOpenSession?: (sessionKey: string) => void;
  }

  const {
    workspace,
    launchTargets,
    sessions,
    launchingKey = null,
    onLaunch,
    onOpenSession,
  }: WorkspaceHomeProps = $props();

  const visibleTargets = $derived(
    launchTargets.filter((target) => target.kind !== "plain_shell"),
  );

  function title(): string {
    return workspace.mr_title ?? workspace.git_head_ref;
  }

  function sourceLabel(target: LaunchTarget): string {
    if (target.source === "config") return "configured";
    if (target.kind === "tmux") return "tmux";
    return "detected";
  }
</script>

<section class="workspace-home" aria-label="Worktree Home">
  <div class="home-header">
    <div class="home-title-block">
      <h2>{title()}</h2>
      <div class="home-meta">
        <span>{workspace.repo_owner}/{workspace.repo_name} #{workspace.item_number}</span>
        <span>{workspace.git_head_ref}</span>
      </div>
    </div>
    <code class="worktree-path">{workspace.worktree_path}</code>
  </div>

  <div class="home-section">
    <div class="section-title">Launch</div>
    <div class="launch-grid">
      {#each visibleTargets as target (target.key)}
        <button
          class="launch-card"
          disabled={!target.available || launchingKey === target.key}
          title={target.disabled_reason ?? target.label}
          aria-label={target.label}
          onclick={() => onLaunch?.(target.key)}
        >
          <span class="launch-label">{target.label}</span>
          <span class="launch-source">{sourceLabel(target)}</span>
        </button>
      {/each}
    </div>
  </div>

  {#if sessions.length > 0}
    <div class="home-section">
      <div class="section-title">Running</div>
      <div class="session-list">
        {#each sessions as session (session.key)}
          <button
            class="session-row"
            onclick={() => onOpenSession?.(session.key)}
          >
            <span class="session-dot"></span>
            <span>{session.label} {session.status}</span>
          </button>
        {/each}
      </div>
    </div>
  {/if}
</section>

<style>
  .workspace-home {
    display: flex;
    flex-direction: column;
    gap: 18px;
    padding: 18px;
    min-width: 0;
    height: 100%;
    overflow: auto;
    background: var(--bg-primary);
  }

  .home-header {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 8px;
  }

  .home-title-block {
    min-width: 0;
  }

  h2 {
    margin: 0;
    font-size: 16px;
    line-height: 1.3;
    font-weight: 650;
    color: var(--text-primary);
  }

  .home-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-top: 4px;
    font-size: 12px;
    color: var(--text-muted);
  }

  .worktree-path {
    width: fit-content;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    padding: 4px 7px;
    border: 1px solid var(--border-muted);
    border-radius: 6px;
    background: var(--bg-inset);
    color: var(--text-secondary);
    font-family: var(--font-mono);
    font-size: 12px;
  }

  .home-section {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .section-title {
    font-size: 11px;
    font-weight: 650;
    text-transform: uppercase;
    letter-spacing: 0;
    color: var(--text-muted);
  }

  .launch-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
    gap: 8px;
  }

  .launch-card {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    min-height: 42px;
    padding: 9px 10px;
    border: 1px solid var(--border-default);
    border-radius: 6px;
    background: var(--bg-surface);
    color: var(--text-primary);
    font: inherit;
    cursor: pointer;
  }

  .launch-card:hover:not(:disabled) {
    border-color: var(--accent-blue);
    background: var(--bg-surface-hover);
  }

  .launch-card:disabled {
    cursor: not-allowed;
    color: var(--text-muted);
    opacity: 0.68;
  }

  .launch-label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 13px;
    font-weight: 600;
  }

  .launch-source {
    flex-shrink: 0;
    color: var(--text-muted);
    font-size: 11px;
  }

  .session-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .session-row {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 34px;
    padding: 6px 9px;
    border: 1px solid var(--border-muted);
    border-radius: 6px;
    background: var(--bg-surface);
    color: var(--text-secondary);
    font: inherit;
    font-size: 13px;
    text-align: left;
    cursor: pointer;
  }

  .session-row:hover {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  .session-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--accent-green);
    flex-shrink: 0;
  }
</style>
