<script lang="ts">
  import { onMount } from "svelte";
  import {
    navigate,
    buildItemRoute,
  } from "../../stores/router.svelte.ts";

  interface Workspace {
    id: string;
    platform_host: string;
    repo_owner: string;
    repo_name: string;
    item_type: "pull_request" | "issue";
    item_number: number;
    git_head_ref: string;
    worktree_path: string;
    tmux_session: string;
    tmux_pane_title?: string | null;
    tmux_working?: boolean;
    tmux_activity_source?:
      | "title"
      | "output"
      | "none"
      | "unknown"
      | null;
    tmux_last_output_at?: string | null;
    status: string;
    error_message?: string | null;
    created_at: string;
    mr_title?: string | null;
    mr_state?: string | null;
    mr_is_draft?: boolean | null;
    mr_ci_status?: string | null;
    mr_review_decision?: string | null;
    mr_additions?: number | null;
    mr_deletions?: number | null;
  }

  const { selectedId }: { selectedId: string } = $props();

  const basePath = (
    window.__BASE_PATH__ ?? "/"
  ).replace(/\/$/, "");

  let workspaces = $state.raw<Workspace[]>([]);
  let collapsedGroups = $state<Set<string>>(new Set());

  type GroupedWorkspaces = Map<string, Workspace[]>;

  const grouped: GroupedWorkspaces = $derived.by(() => {
    const map = new Map<string, Workspace[]>();
    for (const ws of workspaces) {
      const key =
        `${ws.platform_host}/${ws.repo_owner}` +
        `/${ws.repo_name}`;
      const list = map.get(key);
      if (list) {
        list.push(ws);
      } else {
        map.set(key, [ws]);
      }
    }
    return map;
  });

  async function fetchWorkspaces(): Promise<void> {
    try {
      const res = await fetch(
        `${basePath}/api/v1/workspaces`,
      );
      if (!res.ok) return;
      const data = (await res.json()) as {
        workspaces: Workspace[];
      };
      workspaces = data.workspaces ?? [];
    } catch {
      // Network error; keep stale list.
    }
  }

  function toggleGroup(key: string): void {
    const next = new Set(collapsedGroups);
    if (next.has(key)) {
      next.delete(key);
    } else {
      next.add(key);
    }
    collapsedGroups = next;
  }

  function displayName(ws: Workspace): string {
    return ws.mr_title ?? ws.git_head_ref;
  }

  function statusColor(ws: Workspace): string {
    if (ws.status === "ready") return "var(--accent-green)";
    if (ws.status === "error") return "var(--accent-red)";
    return "var(--accent-amber)";
  }

  function workingBadgeTitle(ws: Workspace): string {
    const title = ws.tmux_pane_title?.trim();
    const source = ws.tmux_activity_source;
    if (source && source !== "unknown" && title) {
      return `Working (${source}): ${title}`;
    }
    if (source && source !== "unknown") {
      return `Working (${source})`;
    }
    return title || "Working";
  }

  function itemBadgeColor(ws: Workspace): string {
    if (ws.item_type === "issue") {
      return ws.mr_state === "closed"
        ? "var(--accent-red)"
        : "var(--accent-green)";
    }
    if (ws.mr_is_draft) return "var(--text-muted)";
    if (ws.mr_state === "merged") {
      return "var(--accent-purple)";
    }
    if (ws.mr_state === "closed") {
      return "var(--accent-red)";
    }
    return "var(--accent-green)";
  }

  function handleItemClick(
    e: MouseEvent,
    ws: Workspace,
  ): void {
    e.stopPropagation();
    navigate(
      buildItemRoute(
        ws.item_type === "issue" ? "issue" : "pr",
        ws.repo_owner,
        ws.repo_name,
        ws.item_number,
        ws.platform_host,
      ),
    );
  }

  onMount(() => {
    void fetchWorkspaces();
    const pollHandle = window.setInterval(() => {
      void fetchWorkspaces();
    }, 5_000);

    const evtUrl = `${basePath}/api/v1/events`;
    const source = new EventSource(evtUrl);
    source.addEventListener(
      "workspace_status",
      () => {
        void fetchWorkspaces();
      },
    );

    return () => {
      window.clearInterval(pollHandle);
      source.close();
    };
  });
</script>

<div class="workspace-list-sidebar">
  <div class="sidebar-header">Workspaces</div>
  <div class="sidebar-list">
    {#each [...grouped] as [repoKey, items] (repoKey)}
      <button
        class="group-header"
        onclick={() => toggleGroup(repoKey)}
      >
        <span class="chevron"
          >{collapsedGroups.has(repoKey)
            ? "\u25B6"
            : "\u25BC"}</span
        >
        <span class="group-label">{repoKey}</span>
      </button>
      {#if !collapsedGroups.has(repoKey)}
        {#each items as ws (ws.id)}
          <div
            class="ws-row"
            class:selected={ws.id === selectedId}
            onclick={() =>
              navigate(`/terminal/${ws.id}`)}
            onkeydown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                navigate(`/terminal/${ws.id}`);
              }
            }}
            tabindex="0"
            role="button"
          >
            <div class="ws-row-top">
              <span
                class="status-dot"
                class:spinning={ws.status ===
                  "creating"}
                style:background={statusColor(ws)}
              ></span>
              <span class="ws-name">
                {displayName(ws)}
              </span>
              {#if ws.tmux_working}
                <span
                  class="working-badge"
                  title={workingBadgeTitle(ws)}
                >
                  <span
                    class="working-spinner"
                    aria-hidden="true"
                  ></span>
                  Working
                </span>
              {/if}
            </div>
            <div class="ws-row-bottom">
              <button
                class="pr-badge"
                style:color={itemBadgeColor(ws)}
                style:border-color={itemBadgeColor(ws)}
                onclick={(e) => handleItemClick(e, ws)}
              >
                #{ws.item_number}
              </button>
              {#if ws.item_type === "issue"}
                <span class="branch-pill">
                  {ws.git_head_ref}
                </span>
              {:else if ws.mr_additions != null || ws.mr_deletions != null}
                <span class="diff-stats">
                  {#if ws.mr_additions != null}
                    <span class="additions"
                      >+{ws.mr_additions}</span
                    >
                  {/if}
                  {#if ws.mr_deletions != null}
                    <span class="deletions"
                      >-{ws.mr_deletions}</span
                    >
                  {/if}
                </span>
              {/if}
            </div>
          </div>
        {/each}
      {/if}
    {/each}
  </div>
</div>

<style>
  .workspace-list-sidebar {
    width: 100%;
    height: 100%;
    background: var(--bg-inset);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .sidebar-header {
    padding: 12px 14px;
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--text-muted);
    border-bottom: 1px solid var(--border-muted);
  }

  .sidebar-list {
    flex: 1;
    overflow-y: auto;
    padding: 4px 0;
  }

  .group-header {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    padding: 6px 14px;
    font-size: 11px;
    font-weight: 600;
    color: var(--text-secondary);
    text-align: left;
  }

  .group-header:hover {
    background: var(--bg-surface-hover);
  }

  .chevron {
    font-size: 8px;
    width: 10px;
    flex-shrink: 0;
  }

  .group-label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .ws-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
    width: 100%;
    padding: 8px 14px 8px 24px;
    text-align: left;
    border-left: 3px solid transparent;
  }

  .ws-row:hover {
    background: var(--bg-surface-hover);
  }

  .ws-row.selected {
    background: var(--bg-surface);
    border-left-color: var(--accent-blue);
  }

  .ws-row-top {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  .status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }

  .status-dot.spinning {
    animation: pulse 1.2s ease-in-out infinite;
  }

  @keyframes pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.3;
    }
  }

  .ws-name {
    font-size: 13px;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }

  .working-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    flex-shrink: 0;
    padding: 1px 6px;
    border: 1px solid color-mix(in srgb, var(--accent-amber) 55%, transparent);
    border-radius: 999px;
    color: var(--accent-amber);
    background: color-mix(in srgb, var(--accent-amber) 12%, transparent);
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.03em;
    text-transform: uppercase;
    line-height: 1.4;
  }

  .working-spinner {
    width: 7px;
    height: 7px;
    border: 1px solid currentColor;
    border-top-color: transparent;
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  .ws-row-bottom {
    display: flex;
    align-items: center;
    gap: 8px;
    padding-left: 16px;
  }

  .pr-badge {
    font-size: 11px;
    font-weight: 600;
    padding: 1px 6px;
    border: 1px solid;
    border-radius: 10px;
    line-height: 1.4;
  }

  .pr-badge:hover {
    opacity: 0.8;
  }

  .branch-pill {
    font-size: 11px;
    font-family: var(--font-mono);
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .diff-stats {
    font-size: 11px;
    font-family: var(--font-mono);
    display: flex;
    gap: 6px;
  }

  .additions {
    color: var(--accent-green);
  }

  .deletions {
    color: var(--accent-red);
  }
</style>
