<script lang="ts">
  import { onDestroy } from "svelte";
  import { getStores } from "../../context.js";

  interface Props {
    jobId: number;
    jobStatus: string;
  }
  let { jobId, jobStatus }: Props = $props();

  const stores = getStores();
  const logStore = stores.roborevLog;
  let container: HTMLElement | undefined;

  $effect(() => {
    if (!logStore) return;
    logStore.clear();
    if (
      jobStatus === "running" ||
      jobStatus === "queued"
    ) {
      void logStore.startStreaming(jobId);
    } else {
      void logStore.loadSnapshot(jobId);
    }
  });

  $effect(() => {
    if (!logStore || !container) return;
    void logStore.getLines().length;
    if (logStore.getFollowMode()) {
      container.scrollTop = container.scrollHeight;
    }
  });

  onDestroy(() => {
    logStore?.stopStreaming();
  });

  function lineClass(lineType: string): string {
    if (lineType === "stderr") return "line-stderr";
    return "";
  }
</script>

<div class="log-viewer">
  <div class="log-toolbar">
    <span class="log-status">
      {#if logStore?.isStreaming()}
        <span class="streaming-dot"></span>
        Streaming...
      {:else}
        {logStore?.getLines().length ?? 0} lines
      {/if}
    </span>
    <button
      class="follow-btn"
      class:active={logStore?.getFollowMode()}
      onclick={() => logStore?.toggleFollow()}
      title="Auto-scroll to bottom"
    >
      Follow
    </button>
  </div>
  <div
    class="log-container"
    bind:this={container}
  >
    {#if logStore}
      {#each logStore.getLines() as line}
        <div class="log-line {lineClass(line.lineType)}">
          <span class="log-text">{line.text}</span>
        </div>
      {/each}
      {#if !logStore.isStreaming() && logStore.getLines().length === 0}
        <div class="log-empty">
          No log output available.
        </div>
      {/if}
    {:else}
      <div class="log-empty">
        Log store not available.
      </div>
    {/if}
  </div>
</div>

<style>
  .log-viewer {
    display: flex;
    flex-direction: column;
    height: 100%;
  }

  .log-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 12px;
    border-bottom: 1px solid var(--border-muted);
    flex-shrink: 0;
    font-size: 12px;
  }

  .log-status {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--text-secondary);
  }

  .streaming-dot {
    display: inline-block;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent-green);
    animation: pulse 1.5s ease-in-out infinite;
  }

  @keyframes pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.4;
    }
  }

  .follow-btn {
    padding: 2px 10px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-secondary);
    font-size: 11px;
    cursor: pointer;
  }

  .follow-btn:hover {
    background: var(--bg-surface-hover);
  }

  .follow-btn.active {
    background: var(--accent-blue);
    color: #fff;
    border-color: var(--accent-blue);
  }

  .log-container {
    flex: 1;
    overflow-y: auto;
    background: var(--bg-inset);
    padding: 8px 12px;
    font-family: var(--font-mono);
    font-size: 12px;
    line-height: 1.5;
  }

  .log-line {
    white-space: pre-wrap;
    word-break: break-all;
    color: var(--text-primary);
  }

  .line-stderr {
    color: var(--review-failed);
  }

  .log-text {
    user-select: text;
  }

  .log-empty {
    padding: 24px;
    text-align: center;
    color: var(--text-muted);
    font-family: var(--font-sans);
    font-size: 13px;
  }
</style>
