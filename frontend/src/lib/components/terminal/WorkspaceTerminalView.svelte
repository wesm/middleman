<script lang="ts">
  import { onMount } from "svelte";
  import { navigate } from "../../stores/router.svelte.ts";
  import WorkspaceListSidebar from "./WorkspaceListSidebar.svelte";
  import TerminalPane from "./TerminalPane.svelte";
  import { WorkspaceRightSidebar } from "@middleman/ui";

  interface Workspace {
    id: string;
    platform_host: string;
    repo_owner: string;
    repo_name: string;
    mr_number: number;
    mr_head_ref: string;
    worktree_path: string;
    tmux_session: string;
    status: string;
    error_message?: string | null;
    created_at: string;
    mr_title?: string | null;
    mr_state?: string | null;
    mr_is_draft?: boolean | null;
  }

  const {
    workspaceId,
  }: { workspaceId: string } = $props();

  const basePath = (
    window.__BASE_PATH__ ?? "/"
  ).replace(/\/$/, "");

  let workspace = $state<Workspace | null>(null);
  let loadError = $state<string | null>(null);
  let pollTimer = $state<ReturnType<
    typeof setInterval
  > | null>(null);
  let eventSource = $state<EventSource | null>(null);

  const SIDEBAR_TAB_KEY = "middleman-workspace-sidebar-tab";
  const SIDEBAR_OPEN_KEY = "middleman-workspace-sidebar-open";
  const SIDEBAR_WIDTH_KEY = "middleman-workspace-sidebar-width";

  type SidebarTab = "pr" | "reviews";

  function loadSidebarTab(): SidebarTab {
    const v = localStorage.getItem(SIDEBAR_TAB_KEY);
    return v === "reviews" ? "reviews" : "pr";
  }

  function loadSidebarOpen(): boolean {
    return localStorage.getItem(SIDEBAR_OPEN_KEY) === "true";
  }

  const MIN_SIDEBAR_WIDTH = 280;
  const MIN_TERMINAL_WIDTH = 300;
  const DEFAULT_SIDEBAR_WIDTH = 640;

  function loadSidebarWidth(): number {
    const v = parseInt(
      localStorage.getItem(SIDEBAR_WIDTH_KEY) ?? "",
      10,
    );
    return Number.isFinite(v)
      ? Math.max(MIN_SIDEBAR_WIDTH, v)
      : DEFAULT_SIDEBAR_WIDTH;
  }

  let sidebarTab = $state<SidebarTab>(loadSidebarTab());
  let sidebarOpen = $state(loadSidebarOpen());
  let sidebarWidth = $state(loadSidebarWidth());

  $effect(() => {
    localStorage.setItem(SIDEBAR_TAB_KEY, sidebarTab);
  });
  $effect(() => {
    localStorage.setItem(
      SIDEBAR_OPEN_KEY,
      String(sidebarOpen),
    );
  });
  $effect(() => {
    localStorage.setItem(
      SIDEBAR_WIDTH_KEY,
      String(sidebarWidth),
    );
  });

  function handleSegmentClick(tab: SidebarTab): void {
    if (sidebarOpen && sidebarTab === tab) {
      sidebarOpen = false;
    } else {
      sidebarTab = tab;
      sidebarOpen = true;
    }
  }

  function toggleSidebar(): void {
    sidebarOpen = !sidebarOpen;
  }

  let containerEl = $state<HTMLElement | null>(null);

  // Clamp sidebar width to fit container when it
  // becomes available or when the sidebar opens
  $effect(() => {
    if (!containerEl || !sidebarOpen) return;
    const maxW =
      containerEl.clientWidth - MIN_TERMINAL_WIDTH;
    if (sidebarWidth > maxW && maxW > MIN_SIDEBAR_WIDTH) {
      sidebarWidth = maxW;
    }
  });

  function startSidebarResize(e: MouseEvent): void {
    e.preventDefault();
    const startX = e.clientX;
    const startW = sidebarWidth;
    const maxW = containerEl
      ? containerEl.clientWidth - MIN_TERMINAL_WIDTH
      : 9999;

    function onMove(ev: MouseEvent): void {
      sidebarWidth = Math.max(
        MIN_SIDEBAR_WIDTH,
        Math.min(maxW, startW - (ev.clientX - startX)),
      );
    }

    function onUp(): void {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    }

    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
  }

  $effect(() => {
    function onKeydown(e: KeyboardEvent): void {
      if (
        e.key === "]" &&
        (e.metaKey || e.ctrlKey) &&
        !e.defaultPrevented
      ) {
        e.preventDefault();
        toggleSidebar();
      }
    }
    window.addEventListener("keydown", onKeydown);
    return () =>
      window.removeEventListener("keydown", onKeydown);
  });

  function displayName(ws: Workspace): string {
    return ws.mr_title ?? ws.mr_head_ref;
  }

  async function fetchWorkspace(): Promise<void> {
    try {
      const url =
        `${basePath}/api/v1/workspaces` +
        `/${encodeURIComponent(workspaceId)}`;
      const res = await fetch(url);
      if (!res.ok) {
        loadError = `Failed to load workspace (${res.status})`;
        return;
      }
      const data = (await res.json()) as Workspace;
      workspace = data;
      loadError = null;

      if (data.status !== "creating") {
        stopPolling();
      }
    } catch (err) {
      loadError =
        err instanceof Error
          ? err.message
          : "Network error";
    }
  }

  function startPolling(): void {
    if (pollTimer) return;
    pollTimer = setInterval(() => {
      void fetchWorkspace();
    }, 3000);
  }

  function stopPolling(): void {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  async function handleDelete(): Promise<void> {
    const url =
      `${basePath}/api/v1/workspaces` +
      `/${encodeURIComponent(workspaceId)}`;
    const res = await fetch(url, {
      method: "DELETE",
      headers: { "Content-Type": "application/json" },
    });
    if (res.status === 409) {
      const body = await res.json().catch(() => ({})) as {
        detail?: string;
      };
      const msg =
        body.detail ??
        "Workspace has uncommitted changes.";
      if (!confirm(`${msg}\n\nForce delete?`)) return;
      const forceRes = await fetch(
        `${url}?force=true`,
        {
          method: "DELETE",
          headers: { "Content-Type": "application/json" },
        },
      );
      if (!forceRes.ok && forceRes.status !== 204) {
        loadError = `Delete failed (${forceRes.status})`;
        return;
      }
    } else if (!res.ok && res.status !== 204) {
      loadError = `Delete failed (${res.status})`;
      return;
    }
    navigate("/pulls");
  }

  onMount(() => {
    if (!workspaceId) return;

    const evtUrl = `${basePath}/api/v1/events`;
    const source = new EventSource(evtUrl);
    eventSource = source;

    source.addEventListener(
      "workspace_status",
      (e: MessageEvent) => {
        try {
          const data = JSON.parse(
            e.data as string,
          ) as { id?: string };
          if (data.id === workspaceId) {
            void fetchWorkspace();
          }
        } catch {
          // Malformed SSE data; ignore.
        }
      },
    );

    void fetchWorkspace().then(() => {
      if (
        workspace &&
        workspace.status === "creating"
      ) {
        startPolling();
      }
    });

    return () => {
      stopPolling();
      if (eventSource) {
        eventSource.close();
        eventSource = null;
      }
    };
  });
</script>

<div class="terminal-view">
  <WorkspaceListSidebar selectedId={workspaceId} />

  <div class="terminal-main">
    {#if !workspaceId}
      <div class="state-message">
        Select a workspace from the sidebar
      </div>
    {:else if loadError && !workspace}
      <div class="state-message error">
        <span class="error-icon">!</span>
        <span>{loadError}</span>
        <button
          class="retry-btn"
          onclick={() => {
            loadError = null;
            void fetchWorkspace();
          }}
        >
          Retry
        </button>
      </div>
    {:else if !workspace || workspace.status === "creating"}
      <div class="state-message">
        <svg
          class="spinner"
          width="18"
          height="18"
          viewBox="0 0 18 18"
          fill="none"
        >
          <circle
            cx="9"
            cy="9"
            r="7"
            stroke="currentColor"
            stroke-opacity="0.2"
            stroke-width="2"
          />
          <path
            d="M16 9a7 7 0 0 0-7-7"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
          />
        </svg>
        <span>Setting up workspace...</span>
      </div>
    {:else if workspace.status === "error"}
      <div class="state-message error">
        <span class="error-icon">!</span>
        <span>
          {workspace.error_message ??
            "Workspace setup failed"}
        </span>
        <button
          class="retry-btn"
          onclick={() => void fetchWorkspace()}
        >
          Retry
        </button>
      </div>
    {:else}
      <div class="header-bar">
        <div class="header-left">
          <span class="header-name">
            {displayName(workspace)}
          </span>
          <code class="header-branch">
            {workspace.mr_head_ref}
          </code>
        </div>
        <div class="header-right">
          <div class="seg-control">
            <button
              class="seg-btn"
              class:active={sidebarOpen && sidebarTab === "pr"}
              onclick={() => handleSegmentClick("pr")}
            >
              PR
            </button>
            <button
              class="seg-btn"
              class:active={sidebarOpen && sidebarTab === "reviews"}
              onclick={() => handleSegmentClick("reviews")}
            >
              Reviews
            </button>
          </div>
          <button
            class="header-btn danger"
            onclick={() => void handleDelete()}
          >
            Delete
          </button>
        </div>
      </div>
      <div class="terminal-and-sidebar" bind:this={containerEl}>
        <div class="terminal-area">
          <TerminalPane {workspaceId} />
        </div>
        {#if sidebarOpen && workspace}
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div
            class="sidebar-resize-handle"
            onmousedown={startSidebarResize}
          ></div>
          <div
            class="right-sidebar"
            style="width: {sidebarWidth}px"
          >
            <WorkspaceRightSidebar
              activeTab={sidebarTab}
              repoOwner={workspace.repo_owner}
              repoName={workspace.repo_name}
              mrNumber={workspace.mr_number}
              branch={workspace.mr_head_ref}
              roborevBaseUrl={basePath + "/api/roborev"}
            />
          </div>
        {/if}
      </div>
    {/if}
  </div>
</div>

<style>
  .terminal-view {
    display: flex;
    width: 100%;
    height: 100%;
    background: var(--bg-primary);
  }

  .terminal-main {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    min-width: 0;
  }

  .state-message {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    flex: 1;
    color: var(--text-muted);
    font-size: 14px;
  }

  .state-message.error {
    color: var(--accent-red);
  }

  .error-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    background: var(--accent-red);
    color: #fff;
    font-size: 13px;
    font-weight: 700;
    flex-shrink: 0;
  }

  .retry-btn {
    padding: 4px 12px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    background: var(--bg-surface);
    color: var(--text-primary);
    font-size: 12px;
    cursor: pointer;
  }

  .retry-btn:hover {
    background: var(--bg-surface-hover);
  }

  .spinner {
    animation: spin 0.8s linear infinite;
    color: var(--text-muted);
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
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

  .header-btn {
    padding: 4px 12px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    background: var(--bg-surface);
    color: var(--text-secondary);
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
  }

  .header-btn:hover {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  .header-btn.danger:hover {
    background: var(--accent-red);
    color: #fff;
    border-color: var(--accent-red);
  }

  .terminal-area {
    flex: 1;
    overflow: hidden;
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

  .terminal-and-sidebar {
    flex: 1;
    display: flex;
    overflow: hidden;
  }

  .sidebar-resize-handle {
    width: 4px;
    cursor: col-resize;
    background: var(--border-muted);
    flex-shrink: 0;
  }

  .sidebar-resize-handle:hover {
    background: var(--accent-blue);
  }

  .right-sidebar {
    flex-shrink: 0;
    overflow: hidden;
  }
</style>
