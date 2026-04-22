<script lang="ts">
  // Import-pattern: takes `activePlatformHost`, `navigate`, and
  // `onWorkspaceCommand` as callback props — mirrors
  // WorkspacePanelView.svelte. `packages/ui` must not cross-import
  // frontend/src helpers, so the App.svelte consumer wires the
  // concrete implementations (navigate from router, workspace
  // commands from embed-config). `navigate` uses pushState so tab
  // switches are restorable via browser back/forward.
  import WorkspaceRightSidebar
    from "../components/workspace/WorkspaceRightSidebar.svelte";

  type Tab = "pr" | "reviews";

  interface Props {
    platformHost: string;
    owner: string;
    name: string;
    number: number;
    branch?: string | undefined;
    tab?: Tab | undefined;
    basePath: string;
    activePlatformHost: string | null;
    navigate: (path: string) => void;
    onWorkspaceCommand: (
      command: string,
      payload: Record<string, unknown>,
    ) => void;
  }

  let {
    platformHost,
    owner,
    name,
    number,
    branch = "",
    tab,
    basePath,
    activePlatformHost,
    navigate,
    onWorkspaceCommand,
  }: Props = $props();

  const TAB_KEY = "middleman-workspace-sidebar-tab";

  function storedTab(): Tab {
    const v = localStorage.getItem(TAB_KEY);
    return v === "reviews" ? "reviews" : "pr";
  }

  const activeTab: Tab = $derived(
    tab === "pr" || tab === "reviews" ? tab : storedTab(),
  );

  $effect(() => {
    localStorage.setItem(TAB_KEY, activeTab);
  });

  const bp = $derived(basePath.replace(/\/$/, ""));

  const isNonPrimary = $derived(
    activePlatformHost !== null
      && platformHost !== activePlatformHost,
  );

  const headerTitle = $derived(
    number > 0
      ? `${owner}/${name} #${number}`
      : `${owner}/${name}`,
  );

  function rebuildURL(next: Tab): string {
    const base =
      `/workspaces/sidebar/${platformHost}`
      + `/${owner}/${name}/${number}`;
    const params = new URLSearchParams();
    if (branch) params.set("branch", branch);
    params.set("tab", next);
    return `${base}?${params.toString()}`;
  }

  function handleSegment(next: Tab): void {
    if (next === activeTab) return;
    navigate(rebuildURL(next));
  }

  function handleRevealHostSettings(): void {
    onWorkspaceCommand("revealHostSettings", {});
  }
</script>

<div class="workspace-sidebar-view">
  <div class="header-bar">
    <div class="header-left">
      <span class="header-name">{headerTitle}</span>
      {#if branch}
        <code class="header-branch">{branch}</code>
      {/if}
    </div>
    <div class="header-right">
      <div class="seg-control">
        <button
          class="seg-btn"
          class:active={activeTab === "pr"}
          onclick={() => handleSegment("pr")}
        >
          PR
        </button>
        <button
          class="seg-btn"
          class:active={activeTab === "reviews"}
          onclick={() => handleSegment("reviews")}
        >
          Reviews
        </button>
      </div>
    </div>
  </div>
  <div class="body">
    {#if activePlatformHost === null}
      <div class="state-message" data-testid="startup-state">
        Middleman is starting up...
      </div>
    {:else if isNonPrimary}
      <div class="state-message" data-testid="non-primary-state">
        <p>
          This worktree's repository is on
          <strong>{platformHost}</strong>.
        </p>
        <p class="state-muted">
          Pull request data is only available for repositories
          on the active host ({activePlatformHost}).
        </p>
        <button
          class="state-action-btn"
          onclick={handleRevealHostSettings}
        >Reveal in Host Settings</button>
      </div>
    {:else}
      <WorkspaceRightSidebar
        {activeTab}
        repoOwner={owner}
        repoName={name}
        mrNumber={number}
        branch={branch ?? ""}
        roborevBaseUrl={bp + "/api/roborev"}
      />
    {/if}
  </div>
</div>

<style>
  .workspace-sidebar-view {
    display: flex;
    flex-direction: column;
    height: 100%;
    width: 100%;
    background: var(--bg-primary);
  }

  .header-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 14px;
    background: var(--bg-surface);
    border-bottom: 1px solid var(--border-default);
    gap: 12px;
    flex-shrink: 0;
  }

  .header-left {
    display: flex;
    align-items: center;
    gap: 10px;
    overflow: hidden;
  }

  .header-name {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .header-branch {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--text-muted);
    background: var(--bg-inset);
    padding: 2px 6px;
    border-radius: var(--radius-sm);
    white-space: nowrap;
  }

  .header-right {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }

  .seg-control {
    display: flex;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }

  .seg-btn {
    padding: 3px 10px;
    border: none;
    background: none;
    color: var(--text-muted);
    font-size: 11px;
    font-weight: 500;
    cursor: pointer;
    font-family: inherit;
  }

  .seg-btn:first-child {
    border-right: 1px solid var(--border-default);
  }

  .seg-btn:hover {
    color: var(--text-secondary);
    background: var(--bg-surface-hover);
  }

  .seg-btn.active {
    background: var(--accent-blue);
    color: #fff;
  }

  .body {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    min-height: 0;
  }

  .state-message {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 4px;
    flex: 1;
    padding: 16px;
    color: var(--text-muted);
    font-size: 13px;
    text-align: center;
  }

  .state-muted {
    font-size: 12px;
    color: var(--text-muted);
    margin-top: 4px;
  }

  .state-action-btn {
    display: inline-block;
    margin-top: 8px;
    padding: 4px 12px;
    font-size: 12px;
    font-weight: 500;
    color: var(--text-secondary);
    background: var(--bg-surface);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm, 4px);
    cursor: pointer;
  }

  .state-action-btn:hover {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }
</style>
