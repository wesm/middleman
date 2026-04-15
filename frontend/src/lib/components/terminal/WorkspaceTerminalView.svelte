<script lang="ts">
  import { onMount } from "svelte";
  import {
    navigate,
    buildItemRoute,
  } from "../../stores/router.svelte.ts";
  import WorkspaceListSidebar from "./WorkspaceListSidebar.svelte";
  import TerminalPane from "./TerminalPane.svelte";

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

  function navigateToPR(): void {
    if (!workspace) return;
    navigate(
      buildItemRoute(
        "pr",
        workspace.repo_owner,
        workspace.repo_name,
        workspace.mr_number,
      ),
    );
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
          <button
            class="header-btn"
            onclick={navigateToPR}
          >
            View PR
          </button>
          <button
            class="header-btn danger"
            onclick={() => void handleDelete()}
          >
            Delete
          </button>
        </div>
      </div>
      <div class="terminal-area">
        <TerminalPane {workspaceId} />
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
</style>
