<script lang="ts">
  import type { WorkspaceData } from "../../api/types.js";
  import ProjectSection from "./ProjectSection.svelte";
  import SessionSection from "./SessionSection.svelte";
  import ResourceFooter from "./ResourceFooter.svelte";

  interface Props {
    workspaceData: WorkspaceData;
    onCommand: (
      command: string,
      payload: Record<string, unknown>,
    ) => void;
  }

  let { workspaceData, onCommand }: Props = $props();

  const selectedHost = $derived(
    workspaceData.hosts.find(
      (h) => h.key === workspaceData.selectedHostKey,
    ) ?? workspaceData.hosts[0],
  );

  const repositoryProjects = $derived(
    selectedHost?.projects.filter(
      (p) => p.kind === "repository",
    ) ?? [],
  );

  const scratchProjects = $derived(
    selectedHost?.projects.filter(
      (p) => p.kind === "scratch",
    ) ?? [],
  );

  const multipleHosts = $derived(
    workspaceData.hosts.length > 1,
  );

  const singleHostDisconnected = $derived(
    !multipleHosts &&
      selectedHost != null &&
      selectedHost.connectionState !== "connected",
  );

  function selectHost(key: string): void {
    onCommand("selectHost", { hostKey: key });
  }

  function retryHost(key: string): void {
    onCommand("retryHost", { hostKey: key });
  }


  function addRepository(): void {
    onCommand("addRepository", {
      hostKey: selectedHost?.key ?? "",
    });
  }
</script>

<div class="workspace-sidebar">
  {#if multipleHosts}
    <div class="host-switcher">
      {#each workspaceData.hosts as host (host.key)}
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="host-btn"
          class:active={host.key === selectedHost?.key}
          onclick={() => selectHost(host.key)}
          onkeydown={(e: KeyboardEvent) => {
            if (e.target === e.currentTarget && (e.key === "Enter" || e.key === " ")) {
              e.preventDefault();
              selectHost(host.key);
            }
          }}
          role="button"
          tabindex="0"
        >
          <span
            class="connection-dot"
            class:connected={
              host.connectionState === "connected"
            }
            class:connecting={
              host.connectionState === "connecting"
            }
            class:disconnected={
              host.connectionState === "disconnected"
            }
            class:error={
              host.connectionState === "error"
            }
          ></span>
          <span class="host-label">{host.label}</span>
          {#if host.connectionState === "disconnected" || host.connectionState === "error"}
            <button
              class="retry-btn"
              onclick={(e: MouseEvent) => {
                e.stopPropagation();
                retryHost(host.key);
              }}
            >
              Retry
            </button>
          {/if}
        </div>
      {/each}
    </div>
  {/if}

  {#if singleHostDisconnected && selectedHost}
    <div class="single-host-status">
      <span
        class="connection-dot"
        class:connecting={
          selectedHost.connectionState === "connecting"
        }
        class:disconnected={
          selectedHost.connectionState === "disconnected"
        }
        class:error={
          selectedHost.connectionState === "error"
        }
      ></span>
      <span class="status-label">
        {selectedHost.connectionState === "connecting"
          ? "Connecting..."
          : selectedHost.connectionState === "error"
            ? "Connection error"
            : "Disconnected"}
      </span>
      <button
        class="retry-btn"
        onclick={() => retryHost(selectedHost.key)}
      >
        Retry
      </button>
    </div>
  {/if}

  <div class="scroll-area">
    {#if selectedHost}
      {#each scratchProjects as project (project.key)}
        <ProjectSection
          {project}
          hostKey={selectedHost.key}
          selectedWorktreeKey={
            workspaceData.selectedWorktreeKey
          }
          {onCommand}
        />
      {/each}

      {#each repositoryProjects as project (project.key)}
        <ProjectSection
          {project}
          hostKey={selectedHost.key}
          selectedWorktreeKey={
            workspaceData.selectedWorktreeKey
          }
          {onCommand}
        />
      {/each}

      <SessionSection
        sessions={selectedHost.sessions}
        hostKey={selectedHost.key}
        {onCommand}
      />
    {/if}
  </div>

  <div class="footer">
    {#if selectedHost}
      <button class="add-repo-btn" onclick={addRepository}>
        Add Repository
      </button>
    {/if}
    {#if selectedHost}
      <ResourceFooter
        resources={selectedHost.resources}
      />
    {/if}
  </div>
</div>

<style>
  .workspace-sidebar {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: var(--bg-primary);
  }

  .host-switcher {
    display: flex;
    gap: 4px;
    padding: 8px 10px;
    border-bottom: 1px solid var(--border-muted);
    flex-shrink: 0;
  }

  .host-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 4px 10px;
    height: 28px;
    font-size: 12px;
    color: var(--text-secondary);
    background: var(--bg-surface);
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-sm, 4px);
    cursor: pointer;
    transition: background 0.1s;
    white-space: nowrap;
  }

  .host-btn:hover {
    background: var(--bg-surface-hover);
  }

  .host-btn.active {
    background: var(--accent-blue);
    color: #fff;
    border-color: var(--accent-blue);
  }

  .host-label {
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }

  .single-host-status {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 10px;
    font-size: 12px;
    color: var(--text-secondary);
    border-bottom: 1px solid var(--border-muted);
    flex-shrink: 0;
  }

  .status-label {
    flex: 1;
    min-width: 0;
  }

  .connection-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
    background: var(--text-muted);
  }

  .connection-dot.connected {
    background: var(--accent-green);
  }

  .connection-dot.connecting {
    background: var(--accent-amber);
  }

  .connection-dot.disconnected {
    background: var(--text-muted);
  }

  .connection-dot.error {
    background: var(--accent-red);
  }

  .retry-btn {
    flex-shrink: 0;
    padding: 2px 8px;
    font-size: 11px;
    color: var(--accent-amber);
    background: none;
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-sm, 4px);
    cursor: pointer;
  }

  .retry-btn:hover {
    background: var(--bg-surface-hover);
  }

  .scroll-area {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }

  .footer {
    flex-shrink: 0;
    border-top: 1px solid var(--border-muted);
  }

  .add-repo-btn {
    display: block;
    width: 100%;
    padding: 8px 10px;
    font-size: 13px;
    color: var(--text-secondary);
    text-align: center;
    background: var(--bg-surface);
    border: none;
    cursor: pointer;
    transition: background 0.1s;
  }

  .add-repo-btn:hover {
    background: var(--bg-surface-hover);
  }
</style>
