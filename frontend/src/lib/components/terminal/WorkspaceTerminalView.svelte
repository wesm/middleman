<script lang="ts">
  import { navigate } from "../../stores/router.svelte.ts";
  import WorkspaceListSidebar from "./WorkspaceListSidebar.svelte";
  import TerminalPane from "./TerminalPane.svelte";
  import WorkspaceHome from "./WorkspaceHome.svelte";
  import WorkspaceTabs from "./WorkspaceTabs.svelte";
  import LaunchMenu from "./LaunchMenu.svelte";
  import ShellDrawer from "./ShellDrawer.svelte";
  import type { RuntimeSession } from "@middleman/ui/api/types";
  import {
    ensureWorkspaceShell,
    getWorkspaceRuntime,
    launchWorkspaceSession,
    stopWorkspaceSession,
    workspaceSessionWebSocketPath,
    workspaceTmuxWebSocketPath,
    type WorkspaceRuntimeState,
  } from "../../api/workspace-runtime.js";
  import {
    CollapsibleResizableSidebar,
    WorkspaceRightSidebar,
  } from "@middleman/ui";
  import { AlertIcon, SpinnerIcon } from "../../icons.ts";

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
  let runtime = $state.raw<WorkspaceRuntimeState | null>(null);
  // The workspace ID that `runtime` was fetched for. Stored
  // alongside the payload so we never render or operate on
  // sessions/targets that belong to a previous workspace
  // (during the in-place transition between workspaces, runtime
  // briefly outlives the workspace it was fetched for).
  let runtimeForId = $state<string>("");
  let loadError = $state<string | null>(null);
  let actionError = $state<string | null>(null);
  let retryingSetup = $state(false);
  let runtimeError = $state<string | null>(null);
  let pollTimer = $state<ReturnType<
    typeof setInterval
  > | null>(null);
  let eventSource = $state<EventSource | null>(null);
  let activeTabKey = $state("home");
  let tmuxTabOpen = $state(false);
  let tmuxTerminalMounted = $state(false);
  let mountedSessionKeys = $state<string[]>([]);
  let launchingKey = $state<string | null>(null);
  let shellOpen = $state(false);
  let shellLoading = $state(false);

  const SIDEBAR_TAB_KEY = "middleman-workspace-sidebar-tab";
  const SIDEBAR_OPEN_KEY = "middleman-workspace-sidebar-open";
  const SIDEBAR_WIDTH_KEY = "middleman-workspace-sidebar-width";
  const WORKSPACE_LIST_WIDTH_KEY =
    "middleman-workspace-list-sidebar-width";
  const ACTIVE_WORKSPACE_TAB_KEY_PREFIX =
    "middleman-workspace-active-tab:";

  type SidebarTab = "pr" | "issue" | "reviews";

  const MIN_WORKSPACE_LIST_WIDTH = 220;
  const DEFAULT_WORKSPACE_LIST_WIDTH = 260;
  const MAX_WORKSPACE_LIST_WIDTH = 420;

  function clampWorkspaceListWidth(
    value: number,
  ): number {
    return Math.max(
      MIN_WORKSPACE_LIST_WIDTH,
      Math.min(
        MAX_WORKSPACE_LIST_WIDTH,
        Math.round(value),
      ),
    );
  }

  function loadWorkspaceListWidth(): number {
    const value = parseInt(
      localStorage.getItem(WORKSPACE_LIST_WIDTH_KEY) ?? "",
      10,
    );
    return Number.isFinite(value)
      ? clampWorkspaceListWidth(value)
      : DEFAULT_WORKSPACE_LIST_WIDTH;
  }

  function loadSidebarTab(): SidebarTab {
    const v = localStorage.getItem(SIDEBAR_TAB_KEY);
    if (v === "issue") return "issue";
    return v === "reviews" ? "reviews" : "pr";
  }

  function loadSidebarOpen(): boolean {
    return localStorage.getItem(SIDEBAR_OPEN_KEY) === "true";
  }

  const MIN_SIDEBAR_WIDTH = 280;
  const MIN_TERMINAL_WIDTH = 300;
  const DEFAULT_SIDEBAR_WIDTH = 640;
  const RIGHT_SIDEBAR_RESIZE_HANDLE_WIDTH = 4;

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
  let workspaceListWidth = $state(loadWorkspaceListWidth());

  // Runtime is only "live" when both the runtime fetch and the
  // workspace fetch resolve for the current route. Without the
  // workspace.id check, a runtime that lands first for the new
  // workspace can render its sessions/launch targets next to the
  // previous workspace's still-cached header/home data.
  const runtimeLive = $derived(
    runtime !== null &&
      runtimeForId === workspaceId &&
      workspace?.id === workspaceId,
  );
  const runtimeSessions = $derived(
    runtimeLive ? (runtime?.sessions ?? []) : [],
  );
  const launchTargets = $derived(
    runtimeLive ? (runtime?.launch_targets ?? []) : [],
  );
  const shellSession = $derived(
    runtimeLive ? (runtime?.shell_session ?? null) : null,
  );
  const shellSessionActive = $derived(
    shellSession?.status === "running" ||
      shellSession?.status === "starting",
  );
  const activeSession = $derived.by(() => {
    if (!activeTabKey.startsWith("session:")) return null;
    const key = activeTabKey.slice("session:".length);
    return runtimeSessions.find((session) => session.key === key) ?? null;
  });

  // While `workspaceId` has moved on but the previous workspace's
  // data is still on screen (the in-place transition), mutating
  // actions must not run — they would target the new id while the
  // user is looking at the old one. The window is small (≤ a few
  // hundred ms) but observable, so guard every action handler with
  // this and disable the buttons.
  const transitioning = $derived(
    workspaceId !== "" &&
      workspace !== null &&
      workspace.id !== workspaceId,
  );
  const actionsBlocked = $derived(transitioning);

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
  $effect(() => {
    localStorage.setItem(
      WORKSPACE_LIST_WIDTH_KEY,
      String(workspaceListWidth),
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

  function clampRightSidebarWidth(
    containerWidth: number,
  ): void {
    const maxW = Math.max(
      0,
      containerWidth -
        MIN_TERMINAL_WIDTH -
        RIGHT_SIDEBAR_RESIZE_HANDLE_WIDTH,
    );
    if (sidebarWidth > maxW) {
      sidebarWidth = maxW;
    }
  }

  // Keep the terminal usable when the main layout
  // shrinks, including when the left workspace list
  // is resized after the right sidebar is already open.
  $effect(() => {
    if (!containerEl || !sidebarOpen) return;

    clampRightSidebarWidth(containerEl.clientWidth);
  });

  $effect(() => {
    if (!sidebarOpen) return;

    function onResize(): void {
      if (containerEl) {
        clampRightSidebarWidth(containerEl.clientWidth);
      }
    }

    window.addEventListener("resize", onResize);
    return () => {
      window.removeEventListener("resize", onResize);
    };
  });

  function startSidebarResize(e: MouseEvent): void {
    e.preventDefault();
    const startX = e.clientX;
    const startW = sidebarWidth;
    const maxW = containerEl
      ? Math.max(
          0,
          containerEl.clientWidth -
            MIN_TERMINAL_WIDTH -
            RIGHT_SIDEBAR_RESIZE_HANDLE_WIDTH,
        )
      : 9999;
    const minW = Math.min(MIN_SIDEBAR_WIDTH, maxW);

    function onMove(ev: MouseEvent): void {
      sidebarWidth = Math.max(
        minW,
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
    return ws.mr_title ?? ws.git_head_ref;
  }

  function mountSessionTerminal(sessionKey: string): void {
    if (!mountedSessionKeys.includes(sessionKey)) {
      mountedSessionKeys = [...mountedSessionKeys, sessionKey];
    }
  }

  function unmountSessionTerminal(sessionKey: string): void {
    mountedSessionKeys = mountedSessionKeys.filter(
      (key) => key !== sessionKey,
    );
  }

  function isSessionTerminalMounted(
    sessionKey: string,
  ): boolean {
    return mountedSessionKeys.includes(sessionKey);
  }

  function rememberActiveTab(key: string): void {
    if (!workspaceId) return;
    localStorage.setItem(
      `${ACTIVE_WORKSPACE_TAB_KEY_PREFIX}${workspaceId}`,
      key,
    );
  }

  function selectWorkspaceTab(key: string): void {
    activeTabKey = key;
    rememberActiveTab(key);
  }

  function restoreWorkspaceTab(id: string): string {
    const remembered = localStorage.getItem(
      `${ACTIVE_WORKSPACE_TAB_KEY_PREFIX}${id}`,
    );
    return remembered ?? "home";
  }

  function defaultSidebarTab(
    ws: Workspace,
  ): SidebarTab {
    return ws.item_type === "issue" ? "issue" : "pr";
  }

  function isSidebarTabSupported(
    ws: Workspace,
    tab: SidebarTab,
  ): boolean {
    if (ws.item_type === "issue") {
      return tab === "issue";
    }
    return tab === "pr" || tab === "reviews";
  }

  async function fetchWorkspace(): Promise<void> {
    // Capture the id at call time. With workspaceId changing across
    // navigations, a slow in-flight fetch for the previous id could
    // otherwise resolve after a newer fetch and overwrite the new
    // workspace's data with stale content (causing a perceived flash
    // back to the previous workspace).
    const id = workspaceId;
    try {
      const url =
        `${basePath}/api/v1/workspaces` +
        `/${encodeURIComponent(id)}`;
      const res = await fetch(url);
      if (id !== workspaceId) return;
      if (!res.ok) {
        loadError = `Failed to load workspace (${res.status})`;
        return;
      }
      const data = (await res.json()) as Workspace;
      if (id !== workspaceId) return;
      workspace = data;
      loadError = null;
      actionError = null;

      if (data.status !== "creating") {
        stopPolling();
      }
      if (data.status === "ready") {
        void fetchRuntime();
      }
    } catch (err) {
      if (id !== workspaceId) return;
      loadError =
        err instanceof Error
          ? err.message
          : "Network error";
    }
  }

  async function fetchRuntime(): Promise<void> {
    if (!workspaceId) return;
    const id = workspaceId;
    try {
      const data = await getWorkspaceRuntime(id);
      if (id !== workspaceId) return;
      runtime = data;
      runtimeForId = id;
      runtimeError = null;
      if (
        activeTabKey.startsWith("session:") &&
        !activeSession
      ) {
        selectWorkspaceTab("home");
      }
      mountedSessionKeys = mountedSessionKeys.filter(
        (key) =>
          data.sessions.some((session) => session.key === key),
      );
    } catch (err) {
      if (id !== workspaceId) return;
      runtimeError =
        err instanceof Error
          ? err.message
          : "Runtime load failed";
    }
  }

  async function handleLaunch(targetKey: string): Promise<void> {
    if (!workspaceId || launchingKey || actionsBlocked) return;
    const target = launchTargets.find((t) => t.key === targetKey);
    if (target?.kind === "tmux") {
      tmuxTabOpen = true;
      tmuxTerminalMounted = true;
      selectWorkspaceTab("tmux");
      return;
    }

    // Capture id so post-await steps bail if workspace changes mid-launch.
    const id = workspaceId;
    launchingKey = targetKey;
    runtimeError = null;
    try {
      const session = await launchWorkspaceSession(
        id,
        targetKey,
      );
      if (id !== workspaceId) return;
      await fetchRuntime();
      if (id !== workspaceId) return;
      mountSessionTerminal(session.key);
      selectWorkspaceTab(`session:${session.key}`);
    } catch (err) {
      if (id !== workspaceId) return;
      runtimeError =
        err instanceof Error ? err.message : "Launch failed";
    } finally {
      if (id === workspaceId) launchingKey = null;
    }
  }

  function openSession(sessionKey: string): void {
    mountSessionTerminal(sessionKey);
    selectWorkspaceTab(`session:${sessionKey}`);
  }

  async function closeSession(session: RuntimeSession): Promise<void> {
    if (actionsBlocked) return;
    if (
      session.status === "running" &&
      !confirm(`Stop ${session.label}?`)
    ) {
      return;
    }
    const id = workspaceId;
    try {
      await stopWorkspaceSession(id, session.key);
      if (id !== workspaceId) return;
      await fetchRuntime();
      if (id !== workspaceId) return;
      unmountSessionTerminal(session.key);
      if (activeTabKey === `session:${session.key}`) {
        selectWorkspaceTab("home");
      }
    } catch (err) {
      if (id !== workspaceId) return;
      runtimeError =
        err instanceof Error ? err.message : "Stop failed";
    }
  }

  async function toggleShell(): Promise<void> {
    if (shellOpen) {
      shellOpen = false;
      return;
    }
    if (actionsBlocked) return;
    shellOpen = true;
    if (shellLoading) return;

    // Always call ensureWorkspaceShell on open. It is idempotent
    // server-side (returns the existing session if running, starts
    // a fresh one if exited), so trusting the locally-cached
    // shellSessionActive flag would mount a TerminalPane against a
    // shell the server has already torn down — yielding a 404
    // attach + reconnect loop.
    const id = workspaceId;
    shellLoading = true;
    runtimeError = null;
    try {
      await ensureWorkspaceShell(id);
      if (id !== workspaceId) return;
      await fetchRuntime();
    } catch (err) {
      if (id !== workspaceId) return;
      runtimeError =
        err instanceof Error
          ? err.message
          : "Shell launch failed";
    } finally {
      if (id === workspaceId) shellLoading = false;
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

  async function handleRetrySetup(): Promise<void> {
    if (!workspace || retryingSetup || actionsBlocked) return;

    retryingSetup = true;
    actionError = null;
    try {
      const url =
        `${basePath}/api/v1/workspaces` +
        `/${encodeURIComponent(workspaceId)}/retry`;
      const res = await fetch(url, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
      });
      if (!res.ok) {
        actionError = `Retry failed (${res.status})`;
        return;
      }
      workspace = (await res.json()) as Workspace;
      if (workspace.status === "creating") {
        startPolling();
        await fetchWorkspace();
      }
    } catch (err) {
      actionError =
        err instanceof Error
          ? err.message
          : "Retry failed";
    } finally {
      retryingSetup = false;
    }
  }

  async function handleDelete(): Promise<void> {
    if (actionsBlocked) return;
    actionError = null;
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
        actionError = `Delete failed (${forceRes.status})`;
        return;
      }
    } else if (!res.ok && res.status !== 204) {
      actionError = `Delete failed (${res.status})`;
      return;
    }
    navigate("/workspaces");
  }

  $effect(() => {
    if (!workspace) return;
    if (!isSidebarTabSupported(workspace, sidebarTab)) {
      sidebarTab = defaultSidebarTab(workspace);
    }
  });

  // React to workspaceId changes (including / from "" on the
  // bare /workspaces route) without remounting the entire view.
  // Removing the {#key} that previously wrapped this component in
  // App.svelte means the lifecycle is now driven entirely by this
  // effect.
  //
  // Critically, this effect must NOT null out `workspace` or
  // `runtime` between switches: the right sidebar and stage area
  // both gate on those values being non-null, so clearing them
  // would unmount the right sidebar and replace the stage with the
  // "Setting up workspace…" spinner — the flash the user is trying
  // to avoid. Instead we let the previous workspace's data stay on
  // screen until the new fetchWorkspace() resolves and overwrites
  // it in place.
  $effect(() => {
    const id = workspaceId;
    const restoredTab = restoreWorkspaceTab(id);

    // Tab state from the previous workspace can't be valid for a
    // different workspace's runtime, so reset these even though
    // workspace/runtime themselves are kept. shellOpen must reset
    // too: the ShellDrawer's TerminalPane only opens its WebSocket
    // on mount, so leaving the drawer open across a workspace
    // change would route keystrokes to the previous workspace's
    // shell while the user looks at the new workspace.
    activeTabKey = restoredTab;
    tmuxTabOpen = restoredTab === "tmux";
    shellOpen = false;
    launchingKey = null;
    shellLoading = false;

    // Errors/transient flags from the prior workspace should not
    // bleed across — clear them but don't touch workspace/runtime.
    loadError = null;
    actionError = null;
    runtimeError = null;
    tmuxTerminalMounted = restoredTab === "tmux";
    mountedSessionKeys = restoredTab.startsWith("session:")
      ? [restoredTab.slice("session:".length)]
      : [];

    if (!id) {
      // /workspaces route: drop workspace data so the empty-state
      // message renders rather than continuing to show whatever
      // the previous /terminal/{id} session left behind.
      workspace = null;
      runtime = null;
      runtimeForId = "";
      return;
    }

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
          if (data.id === id) {
            void fetchWorkspace();
          }
        } catch {
          // Malformed SSE data; ignore.
        }
      },
    );

    void fetchWorkspace().then(() => {
      if (workspace?.status === "creating") {
        startPolling();
      }
    });

    return () => {
      stopPolling();
      source.close();
      if (eventSource === source) {
        eventSource = null;
      }
    };
  });
</script>

<div class="terminal-view">
  <CollapsibleResizableSidebar
    sidebarWidth={workspaceListWidth}
    minSidebarWidth={MIN_WORKSPACE_LIST_WIDTH}
    maxSidebarWidth={MAX_WORKSPACE_LIST_WIDTH}
    onSidebarResize={(width) => {
      workspaceListWidth = clampWorkspaceListWidth(width);
      requestAnimationFrame(() => {
        if (containerEl) {
          clampRightSidebarWidth(containerEl.clientWidth);
        }
      });
    }}
    mainOverflow="hidden"
  >
    {#snippet sidebar()}
      <WorkspaceListSidebar
        selectedId={workspaceId}
        onOpenItemSidebar={(targetId, tab) => {
          // Cross-workspace click: navigate first, then ensure
          // the sidebar is open for the target tab.
          if (targetId !== workspaceId) {
            sidebarTab = tab;
            sidebarOpen = true;
            navigate(`/terminal/${targetId}`);
            return;
          }
          // Same-workspace click: toggle, mirroring the seg-btn
          // behavior in handleSegmentClick.
          if (sidebarOpen && sidebarTab === tab) {
            sidebarOpen = false;
            return;
          }
          sidebarTab = tab;
          sidebarOpen = true;
        }}
      />
    {/snippet}
    <div class="terminal-main">
      {#if !workspaceId}
        <div class="state-message">
          Select a workspace from the sidebar
        </div>
      {:else if loadError && !workspace}
        <div class="state-message error">
          <AlertIcon
            class="error-icon"
            size="16"
            strokeWidth="2"
            aria-label="Workspace load failed"
          />
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
          <SpinnerIcon
            class="spinner"
            size="18"
            strokeWidth="2"
            aria-hidden="true"
          />
          <span>Setting up workspace...</span>
        </div>
      {:else if workspace.status === "error"}
        <div class="state-message error">
          <AlertIcon
            class="error-icon"
            size="16"
            strokeWidth="2"
            aria-label="Workspace setup failed"
          />
          <span>
            {workspace.error_message ??
              "Workspace setup failed"}
          </span>
          <button
            class="retry-btn"
            disabled={retryingSetup}
            onclick={() => void handleRetrySetup()}
          >
            Retry
          </button>
          <button
            class="retry-btn danger"
            onclick={() => void handleDelete()}
          >
            Delete
          </button>
          {#if actionError}
            <span class="action-error">{actionError}</span>
          {/if}
        </div>
      {:else}
        <div class="header-bar">
          <div class="header-left">
            <span class="header-name">
              {displayName(workspace)}
            </span>
            <code class="header-branch">
              {workspace.git_head_ref}
            </code>
          </div>
          <div class="header-right">
            <div class="seg-control">
              {#if workspace.item_type === "issue"}
                <button
                  class="seg-btn"
                  class:active={sidebarOpen && sidebarTab === "issue"}
                  onclick={() => handleSegmentClick("issue")}
                >
                  Issue
                </button>
              {:else}
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
              {/if}
            </div>
            <button
              class="header-btn danger"
              disabled={actionsBlocked}
              onclick={() => void handleDelete()}
            >
              Delete
            </button>
          </div>
        </div>
        <div class="terminal-and-sidebar" bind:this={containerEl}>
          <div class="terminal-area">
            <div class="workspace-surface">
              <div class="workspace-toolbar">
                <WorkspaceTabs
                  activeKey={activeTabKey}
                  sessions={runtimeSessions}
                  tmuxOpen={tmuxTabOpen}
                  onSelectHome={() => {
                    selectWorkspaceTab("home");
                  }}
                  onSelectTmux={() => {
                    tmuxTerminalMounted = true;
                    selectWorkspaceTab("tmux");
                  }}
                  onSelectSession={openSession}
                  onCloseTmux={() => {
                    tmuxTabOpen = false;
                    tmuxTerminalMounted = false;
                    if (activeTabKey === "tmux") {
                      selectWorkspaceTab("home");
                    }
                  }}
                  onCloseSession={(key) => {
                    const session = runtimeSessions.find(
                      (s) => s.key === key,
                    );
                    if (session) void closeSession(session);
                  }}
                />
                <div class="workspace-actions">
                  <LaunchMenu
                    launchTargets={launchTargets}
                    {launchingKey}
                    onLaunch={(key) => void handleLaunch(key)}
                  />
                </div>
              </div>
              {#if runtimeError}
                <div class="runtime-error">{runtimeError}</div>
              {/if}
              <div class="workspace-stage">
                {#if !runtimeLive}
                  <div class="state-message">
                    <SpinnerIcon
                      class="spinner"
                      size="18"
                      strokeWidth="2"
                      aria-hidden="true"
                    />
                    <span>Loading workspace runtime...</span>
                  </div>
                {:else}
                  <div
                    class="stage-pane"
                    class:active={activeTabKey === "home"}
                  >
                    <WorkspaceHome
                      {workspace}
                      launchTargets={launchTargets}
                      sessions={runtimeSessions}
                      {launchingKey}
                      onLaunch={(key) => void handleLaunch(key)}
                      onOpenSession={openSession}
                    />
                  </div>
                  {#if tmuxTabOpen}
                    <div
                      class="stage-pane"
                      class:active={activeTabKey === "tmux"}
                    >
                      {#if tmuxTerminalMounted}
                        <TerminalPane
                          websocketPath={workspaceTmuxWebSocketPath(
                            workspaceId,
                          )}
                          reconnectOnExit={true}
                          active={activeTabKey === "tmux"}
                        />
                      {/if}
                    </div>
                  {/if}
                  {#each runtimeSessions as session (session.key)}
                    <div
                      class="stage-pane"
                      class:active={activeTabKey === `session:${session.key}`}
                    >
                      {#if isSessionTerminalMounted(session.key)}
                        <TerminalPane
                          websocketPath={workspaceSessionWebSocketPath(
                            workspaceId,
                            session.key,
                          )}
                          reconnectOnExit={false}
                          active={activeTabKey === `session:${session.key}`}
                          onExit={() => void fetchRuntime()}
                          initialStatus={session.status}
                        />
                      {/if}
                    </div>
                  {/each}
                {/if}
              </div>
              <ShellDrawer
                {workspaceId}
                open={shellOpen}
                loading={shellLoading}
                shellSession={shellSessionActive ? shellSession : null}
                onToggle={() => void toggleShell()}
                onExit={() => void fetchRuntime()}
              />
            </div>
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
                platformHost={workspace.platform_host}
                repoOwner={workspace.repo_owner}
                repoName={workspace.repo_name}
                itemType={workspace.item_type}
                itemNumber={workspace.item_number}
                branch={workspace.git_head_ref}
                roborevBaseUrl={basePath + "/api/roborev"}
              />
            </div>
          {/if}
        </div>
      {/if}
    </div>
  </CollapsibleResizableSidebar>
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

  :global(.error-icon) {
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

  .retry-btn:disabled {
    opacity: 0.6;
    cursor: wait;
  }

  .retry-btn.danger:hover {
    background: var(--accent-red);
    border-color: var(--accent-red);
    color: #fff;
  }

  .action-error {
    color: var(--accent-red);
    font-size: 12px;
  }

  :global(.spinner) {
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
    height: 34px;
    padding: 0 10px;
    background: var(--bg-surface);
    border-bottom: 1px solid var(--border-default);
    gap: 10px;
    flex-shrink: 0;
  }

  .header-left {
    display: flex;
    align-items: center;
    gap: 8px;
    overflow: hidden;
  }

  .header-name {
    font-size: 12.5px;
    font-weight: 600;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    letter-spacing: 0.005em;
  }

  .header-branch {
    font-family: var(--font-mono);
    font-size: 11.5px;
    color: var(--text-secondary);
    background: var(--bg-inset);
    padding: 1px 6px;
    border-radius: 3px;
    border: 1px solid var(--border-muted);
    white-space: nowrap;
    line-height: 1.5;
  }

  .header-right {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }

  .header-btn {
    height: 22px;
    padding: 0 10px;
    border: 1px solid var(--border-default);
    border-radius: 3px;
    background: var(--bg-surface);
    color: var(--text-secondary);
    font-size: 11.5px;
    font-weight: 500;
    cursor: pointer;
    transition: background-color 80ms ease, color 80ms ease,
      border-color 80ms ease;
  }

  .header-btn:hover {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
    border-color: color-mix(in srgb, var(--text-muted) 40%, var(--border-default));
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

  .workspace-surface {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-width: 0;
    background: var(--bg-primary);
  }

  .workspace-toolbar {
    display: flex;
    align-items: stretch;
    justify-content: space-between;
    gap: 10px;
    height: 30px;
    padding: 0 6px 0 0;
    border-bottom: 1px solid var(--border-default);
    background: var(--bg-inset);
    flex-shrink: 0;
  }

  .workspace-actions {
    display: flex;
    align-items: center;
    gap: 4px;
    flex-shrink: 0;
    padding-left: 6px;
    border-left: 1px solid var(--border-muted);
  }

  .runtime-error {
    padding: 6px 10px;
    border-bottom: 1px solid var(--border-default);
    background: color-mix(in srgb, var(--accent-red) 12%, var(--bg-surface));
    color: var(--accent-red);
    font-size: 12px;
  }

  .workspace-stage {
    position: relative;
    flex: 1;
    min-height: 0;
    overflow: hidden;
  }

  /* Tabs stay mounted across switches so xterm scrollback and the
   * WebSocket survive — non-active panes are layered below and
   * hidden via visibility so layout/sizing is preserved. */
  .stage-pane {
    position: absolute;
    inset: 0;
    visibility: hidden;
  }

  .stage-pane.active {
    visibility: visible;
    z-index: 1;
  }

  .seg-control {
    display: inline-flex;
    height: 22px;
    border: 1px solid var(--border-default);
    border-radius: 3px;
    overflow: hidden;
    background: var(--bg-surface);
  }

  .seg-btn {
    display: inline-flex;
    align-items: center;
    padding: 0 10px;
    border: none;
    background: transparent;
    color: var(--text-secondary);
    font-size: 11px;
    font-weight: 500;
    letter-spacing: 0.01em;
    cursor: pointer;
    font-family: inherit;
    transition: background-color 80ms ease, color 80ms ease;
  }

  .seg-btn + .seg-btn {
    border-left: 1px solid var(--border-default);
  }

  .seg-btn:hover:not(.active) {
    color: var(--text-primary);
    background: var(--bg-surface-hover);
  }

  .seg-btn.active {
    background: var(--accent-blue);
    color: #fff;
    font-weight: 600;
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
