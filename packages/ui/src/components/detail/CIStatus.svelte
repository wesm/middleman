<script lang="ts">
  import type { CICheck } from "../../api/types.js";
  import Chip from "../shared/Chip.svelte";

  interface Props {
    status: string;
    checksJSON: string;
    detailLoaded: boolean;
    detailSyncing: boolean;
    expanded?: boolean;
    showButton?: boolean;
    showPanel?: boolean;
  }

  let {
    status,
    checksJSON,
    detailLoaded,
    detailSyncing,
    expanded = $bindable(false),
    showButton = true,
    showPanel = true,
  }: Props = $props();

  const checks = $derived(parseCIChecks(checksJSON));
  const failedChecks = $derived(
    checks.filter((check) => check.conclusion === "failure"),
  );
  const nonFailedChecks = $derived(
    checks.filter((check) => check.conclusion !== "failure"),
  );
  const hasCI = $derived(Boolean(status || checks.length > 0));

  function parseCIChecks(json: string): CICheck[] {
    if (!json) return [];
    try {
      return JSON.parse(json) as CICheck[];
    } catch {
      return [];
    }
  }

  function checkIcon(check: CICheck): string {
    if (check.status !== "completed") return "◦";
    if (check.conclusion === "success") return "✓";
    if (check.conclusion === "failure") return "✗";
    if (check.conclusion === "skipped" || check.conclusion === "neutral") {
      return "–";
    }
    return "?";
  }

  function checkColor(check: CICheck): string {
    if (check.status !== "completed") return "var(--accent-amber)";
    if (check.conclusion === "success") return "var(--accent-green)";
    if (check.conclusion === "failure") return "var(--accent-red)";
    return "var(--text-muted)";
  }

  function chipColor(chipStatus: string): string {
    if (chipStatus === "success") return "chip--green";
    if (chipStatus === "failure" || chipStatus === "error") return "chip--red";
    if (chipStatus === "pending") return "chip--amber";
    return "chip--muted";
  }

  function formatDuration(seconds: number | undefined): string {
    if (seconds === undefined || seconds < 0 || !Number.isFinite(seconds)) {
      return "";
    }
    const wholeSeconds = Math.floor(seconds);
    if (wholeSeconds < 60) return `${wholeSeconds}s`;
    const minutes = Math.floor(wholeSeconds / 60);
    const remainingSeconds = wholeSeconds % 60;
    if (minutes < 60) {
      return remainingSeconds === 0
        ? `${minutes}m`
        : `${minutes}m ${remainingSeconds}s`;
    }
    const hours = Math.floor(minutes / 60);
    const remainingMinutes = minutes % 60;
    return remainingMinutes === 0 ? `${hours}h` : `${hours}h ${remainingMinutes}m`;
  }
</script>

{#snippet checkRow(check: CICheck)}
  {@const duration = formatDuration(check.duration_seconds)}
  {#if check.url}
    <a
      class="ci-check"
      href={check.url}
      target="_blank"
      rel="noopener noreferrer"
    >
      <span class="ci-icon" style="color: {checkColor(check)}">{checkIcon(check)}</span>
      <span class="ci-name">{check.name}</span>
      {#if duration}
        <span class="ci-duration">{duration}</span>
      {/if}
      {#if check.app}
        <span class="ci-app">{check.app}</span>
      {/if}
      <span class="ci-arrow">→</span>
    </a>
  {:else}
    <div class="ci-check ci-check--static">
      <span class="ci-icon" style="color: {checkColor(check)}">{checkIcon(check)}</span>
      <span class="ci-name">{check.name}</span>
      {#if duration}
        <span class="ci-duration">{duration}</span>
      {/if}
      {#if check.app}
        <span class="ci-app">{check.app}</span>
      {/if}
    </div>
  {/if}
{/snippet}

{#if hasCI}
  <div class="ci-status">
    {#if showButton}
      <Chip
        interactive={true}
        class={chipColor(status)}
        onclick={() => { expanded = !expanded; }}
        title={expanded ? "Collapse CI checks" : "Expand CI checks"}
        {expanded}
      >
        CI: {status || "unknown"}
        {#if checks.length > 0}
          ({checks.length})
        {/if}
        <span class="chip-chevron" class:chip-chevron--open={expanded}>▾</span>
      </Chip>
    {/if}

    {#if showPanel && expanded}
      <div class="ci-collapse">
        {#if !detailLoaded}
          {#if detailSyncing}
            <div class="loading-placeholder">
              <svg class="sync-spinner" width="14" height="14" viewBox="0 0 16 16" fill="none">
                <circle cx="8" cy="8" r="6" stroke="currentColor" stroke-width="2" stroke-dasharray="28" stroke-dashoffset="8" stroke-linecap="round"/>
              </svg>
              Loading checks...
            </div>
          {:else}
            <div class="loading-placeholder">Detail not yet loaded</div>
          {/if}
        {:else if checks.length > 0}
          <div class="ci-checks">
            {#if failedChecks.length > 0}
              <div class="ci-section-label ci-section-label--red">Failed ({failedChecks.length})</div>
              {#each failedChecks as check (check)}
                {@render checkRow(check)}
              {/each}
            {/if}
            {#each nonFailedChecks as check (check)}
              {@render checkRow(check)}
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  </div>
{/if}

<style>
  .ci-status {
    display: contents;
  }

  .chip-chevron {
    font-size: var(--font-size-2xs);
    transition: transform 0.15s;
  }

  .chip-chevron--open {
    transform: rotate(180deg);
  }

  .ci-collapse {
    flex-basis: 100%;
    width: 100%;
    min-width: 0;
    margin-top: 4px;
  }

  .ci-checks {
    display: flex;
    flex-direction: column;
    width: 100%;
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-md);
    overflow: auto;
    flex-shrink: 0;
    max-height: min(320px, 50vh);
  }

  .ci-section-label {
    font-size: var(--font-size-2xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    padding: 6px 12px 4px;
    color: var(--text-muted);
  }

  .ci-section-label--red {
    color: var(--accent-red);
  }

  .ci-check {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 12px;
    font-size: var(--font-size-sm);
    color: var(--text-primary);
    text-decoration: none;
  }

  .ci-check:hover {
    background: var(--bg-surface-hover);
    text-decoration: none;
  }

  .ci-check--static {
    cursor: default;
  }

  .ci-check--static:hover {
    background: transparent;
  }

  .ci-check + .ci-check {
    border-top: 1px solid var(--border-muted);
  }

  .ci-icon {
    font-weight: 700;
    font-size: var(--font-size-root);
    flex-shrink: 0;
    width: 16px;
    text-align: center;
  }

  .ci-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .ci-app {
    font-size: var(--font-size-2xs);
    color: var(--text-muted);
    flex-shrink: 0;
  }

  .ci-duration {
    font-variant-numeric: tabular-nums;
    font-size: var(--font-size-2xs);
    color: var(--text-muted);
    flex-shrink: 0;
  }

  .ci-arrow {
    color: var(--text-muted);
    flex-shrink: 0;
    font-size: var(--font-size-sm);
  }

  .loading-placeholder {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    color: var(--text-muted);
    font-size: var(--font-size-sm);
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-md);
    padding: 10px 12px;
    white-space: nowrap;
  }

  .sync-spinner {
    animation: spin 0.9s linear infinite;
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }
</style>
