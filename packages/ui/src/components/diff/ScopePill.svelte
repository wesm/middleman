<script lang="ts">
  import type { DiffScope } from "../../stores/diff.svelte.js";

  interface Props {
    scope: DiffScope;
    onreset: () => void;
  }

  const { scope, onreset }: Props = $props();

  const dirty = $derived(scope.kind !== "head");

  const label = $derived.by(() => {
    if (scope.kind === "head") return "HEAD";
    if (scope.kind === "commit") return scope.sha.slice(0, 7);
    return `${scope.fromSha.slice(0, 7)}..${scope.toSha.slice(0, 7)}`;
  });
</script>

<button
  class="scope-pill"
  class:scope-pill--dirty={dirty}
  onclick={dirty ? onreset : undefined}
  disabled={!dirty}
  title={dirty ? "Reset to full diff" : "Viewing full diff"}
>
  <span class="scope-pill__dot"></span>
  <span class="scope-pill__label">{label}</span>
  {#if dirty}
    <span class="scope-pill__reset" aria-hidden="true">&times;</span>
  {/if}
</button>

<style>
  .scope-pill {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: var(--font-size-2xs);
    padding: 2px 8px;
    border-radius: 999px;
    line-height: 1.4;
    cursor: default;
    color: var(--text-secondary);
    background: var(--diff-bg);
    border: 1px solid var(--diff-border);
    font-family: var(--font-sans);
    user-select: none;
  }

  .scope-pill__dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    flex-shrink: 0;
    background: var(--accent-green);
  }

  .scope-pill--dirty {
    cursor: pointer;
    color: var(--text-primary);
    background: rgba(251, 191, 36, 0.08);
    border-color: rgba(251, 191, 36, 0.35);
  }

  .scope-pill--dirty:hover {
    background: rgba(251, 191, 36, 0.15);
    border-color: rgba(251, 191, 36, 0.55);
  }

  .scope-pill--dirty .scope-pill__dot {
    background: var(--accent-amber);
  }

  .scope-pill__label {
    font-family: var(--font-mono);
    font-size: var(--font-size-2xs);
  }

  .scope-pill__reset {
    color: var(--text-muted);
    font-size: var(--font-size-xs);
    line-height: 1;
    margin-left: 2px;
  }
</style>
