<script lang="ts">
  import type { RepoMetric } from "./repoSummary.js";

  interface Props {
    metrics: RepoMetric[];
    compact?: boolean;
  }

  let { metrics, compact = false }: Props = $props();
</script>

<div class={["repo-metrics", { "repo-metrics--compact": compact }]}>
  {#each metrics as metric (metric.label)}
    {#if metric.onclick}
      <button
        type="button"
        class={[
          "repo-metric",
          "repo-metric--action",
          `repo-metric--${metric.tone ?? "neutral"}`,
        ]}
        onclick={metric.onclick}
      >
        <span class="repo-metric__value">{metric.value}</span>
        <span class="repo-metric__label">{metric.label}</span>
      </button>
    {:else}
      <div class={["repo-metric", `repo-metric--${metric.tone ?? "neutral"}`]}>
        <span class="repo-metric__value">{metric.value}</span>
        <span class="repo-metric__label">{metric.label}</span>
      </div>
    {/if}
  {/each}
</div>

<style>
  .repo-metrics {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 8px;
  }

  .repo-metrics--compact {
    grid-template-columns: repeat(5, minmax(0, 1fr));
    gap: 0;
    border-top: 1px solid var(--border-muted);
    border-bottom: 1px solid var(--border-muted);
  }

  .repo-metric {
    min-width: 0;
    padding: 10px 12px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    background: var(--bg-surface);
    text-align: left;
  }

  .repo-metrics--compact .repo-metric {
    border: 0;
    border-radius: 0;
    background: transparent;
  }

  .repo-metric--action {
    cursor: pointer;
    font: inherit;
  }

  .repo-metric--action:hover {
    background: var(--bg-surface-hover);
  }

  .repo-metrics--compact .repo-metric:not(:last-child) {
    border-right: 1px solid var(--border-muted);
  }

  .repo-metric__value {
    display: block;
    margin-bottom: 2px;
    color: var(--text-primary);
    font-size: 18px;
    font-weight: 700;
    line-height: 1;
  }

  .repo-metric__label {
    display: block;
    color: var(--text-secondary);
    font-size: 12px;
    line-height: 1.2;
  }

  .repo-metric--blue .repo-metric__value {
    color: var(--accent-blue);
  }

  .repo-metric--amber .repo-metric__value {
    color: var(--accent-amber);
  }

  .repo-metric--green .repo-metric__value {
    color: var(--accent-green);
  }

  @media (max-width: 760px) {
    .repo-metrics,
    .repo-metrics--compact {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .repo-metrics--compact .repo-metric {
      border-bottom: 1px solid var(--border-muted);
    }

    .repo-metrics--compact .repo-metric:nth-child(2n) {
      border-right: 0;
    }
  }
</style>
