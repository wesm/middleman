<script lang="ts">
  import type { Label } from "../../api/types.js";

  interface Props {
    catalogLabels: Label[];
    selectedLabels: Label[];
    syncing?: boolean;
    pendingLabel?: string | null;
    error?: string | null;
    ontoggle: (name: string) => void | Promise<void>;
    onclose: () => void;
  }

  const {
    catalogLabels,
    selectedLabels,
    syncing = false,
    pendingLabel = null,
    error = null,
    ontoggle,
    onclose,
  }: Props = $props();

  let query = $state("");

  const selectedNames = $derived(new Set(selectedLabels.map((label) => label.name)));
  const filteredLabels = $derived.by(() => {
    const needle = query.trim().toLowerCase();
    if (needle === "") return catalogLabels;
    return catalogLabels.filter((label) =>
      `${label.name} ${label.description ?? ""}`.toLowerCase().includes(needle),
    );
  });

  function labelStyle(label: Label): string {
    const color = (label.color || "6e7781").replace(/^#/, "");
    return `--label-color: #${color}`;
  }
</script>

<div class="label-picker" role="dialog" aria-label="Edit labels">
  <div class="label-picker__header">
    <div>
      <strong>Edit labels</strong>
      {#if syncing}
        <span class="label-picker__syncing">Refreshing…</span>
      {/if}
    </div>
    <button type="button" class="label-picker__close" aria-label="Close label picker" onclick={onclose}>×</button>
  </div>

  <label class="label-picker__filter">
    <span>Filter labels</span>
    <input bind:value={query} type="search" placeholder="Filter labels" aria-label="Filter labels" />
  </label>

  {#if error}
    <div class="label-picker__error" role="alert">{error}</div>
  {/if}

  <div class="label-picker__list" role="menu" aria-label="Repository labels">
    {#each filteredLabels as label (label.name)}
      {@const selected = selectedNames.has(label.name)}
      <button
        type="button"
        class="label-picker__row"
        role="menuitemcheckbox"
        aria-checked={selected}
        disabled={pendingLabel !== null}
        onclick={() => ontoggle(label.name)}
      >
        <span class="label-picker__check" aria-hidden="true">{selected ? "✓" : ""}</span>
        <span class="label-picker__color" style={labelStyle(label)} aria-hidden="true"></span>
        <span class="label-picker__text">
          <span class="label-picker__name">{label.name}</span>
          {#if label.description}
            <span class="label-picker__description">{label.description}</span>
          {/if}
        </span>
        {#if pendingLabel === label.name}
          <span class="label-picker__pending">Saving…</span>
        {/if}
      </button>
    {:else}
      <div class="label-picker__empty">No labels found</div>
    {/each}
  </div>
</div>

<style>
  .label-picker {
    width: min(360px, calc(100vw - 32px));
    max-height: min(520px, calc(100vh - 64px));
    display: flex;
    flex-direction: column;
    overflow: hidden;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-lg);
    background: var(--bg-surface);
    box-shadow: var(--shadow-lg);
    color: var(--text-primary);
  }

  .label-picker__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 12px;
    border-bottom: 1px solid var(--border-muted);
  }

  .label-picker__syncing,
  .label-picker__pending {
    margin-left: 4px;
    color: var(--text-muted);
    font-size: var(--font-size-xs);
  }

  .label-picker__close {
    border: 0;
    background: transparent;
    color: var(--text-secondary);
    cursor: pointer;
    font-size: var(--font-size-xl);
    line-height: 1;
  }

  .label-picker__filter {
    display: grid;
    gap: 4px;
    padding: 12px;
    border-bottom: 1px solid var(--border-muted);
    color: var(--text-secondary);
    font-size: var(--font-size-sm);
  }

  .label-picker__filter input {
    width: 100%;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    background: var(--bg-surface);
    color: var(--text-primary);
    padding: 8px 10px;
    font: inherit;
  }

  .label-picker__error {
    margin: 8px 12px 0;
    border: 1px solid var(--accent-red);
    border-radius: var(--radius-md);
    color: var(--accent-red);
    padding: 8px;
    font-size: var(--font-size-sm);
  }

  .label-picker__list {
    overflow: auto;
    padding: 4px 0;
  }

  .label-picker__row {
    width: 100%;
    display: grid;
    grid-template-columns: 18px 14px minmax(0, 1fr) auto;
    align-items: center;
    gap: 8px;
    border: 0;
    background: transparent;
    color: inherit;
    cursor: pointer;
    padding: 8px 12px;
    text-align: left;
  }

  .label-picker__row:hover:not(:disabled),
  .label-picker__row:focus-visible {
    background: var(--bg-surface-hover);
  }

  .label-picker__row:disabled {
    cursor: wait;
    opacity: 0.7;
  }

  .label-picker__check {
    color: var(--accent-green);
    font-weight: 700;
  }

  .label-picker__color {
    width: 12px;
    height: 12px;
    border-radius: 999px;
    background: var(--label-color);
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--label-color), black 20%);
  }

  .label-picker__text {
    min-width: 0;
    display: grid;
    gap: 2px;
  }

  .label-picker__name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 600;
  }

  .label-picker__description {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-secondary);
    font-size: var(--font-size-xs);
  }

  .label-picker__empty {
    padding: 20px 12px;
    color: var(--text-secondary);
    text-align: center;
  }
</style>
