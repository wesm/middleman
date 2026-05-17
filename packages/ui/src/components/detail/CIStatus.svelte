<script lang="ts">
  import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
  import LoaderCircleIcon from "@lucide/svelte/icons/loader-circle";
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

  function isPendingCheck(check: CICheck): boolean {
    return check.status !== "completed";
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
      <span class="ci-icon" style="color: {checkColor(check)}">
        {#if isPendingCheck(check)}
          <span class="sync-spinner" aria-hidden="true">
            <LoaderCircleIcon size={12} strokeWidth={2} />
          </span>
        {:else}
          {checkIcon(check)}
        {/if}
      </span>
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
      <span class="ci-icon" style="color: {checkColor(check)}">
        {#if isPendingCheck(check)}
          <span class="sync-spinner" aria-hidden="true">
            <LoaderCircleIcon size={12} strokeWidth={2} />
          </span>
        {:else}
          {checkIcon(check)}
        {/if}
      </span>
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
        <ChevronDownIcon
          class={["chip-chevron", expanded && "chip-chevron--open"].filter(Boolean).join(" ")}
          size={12}
          strokeWidth={2.4}
          aria-hidden="true"
        />
      </Chip>
    {/if}

    {#if showPanel && expanded}
      <div class="ci-collapse">
        {#if !detailLoaded}
          {#if detailSyncing}
            <div class="loading-placeholder">
              <span class="sync-spinner" aria-hidden="true">
                <LoaderCircleIcon size={14} strokeWidth={2} />
              </span>
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

  :global(.chip-chevron) {
    flex-shrink: 0;
    transform: translateY(1px);
    transition: transform 0.15s;
  }

  :global(.chip-chevron--open) {
    transform: translateY(1px) rotate(180deg);
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
    max-height: min(340px, 50vh);
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
    display: inline-flex;
    align-items: center;
    justify-content: center;
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
    display: inline-flex;
    animation: spin 0.9s linear infinite;
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }
</style>
