<script lang="ts">
  import type { Snippet } from "svelte";

  type Size = "sm" | "md";

  interface Props {
    size?: Size;
    interactive?: boolean;
    title?: string;
    expanded?: boolean;
    disabled?: boolean;
    class?: string;
    onclick?: (event: MouseEvent) => void;
    children?: Snippet;
  }

  let {
    size = "md",
    interactive = false,
    title = undefined,
    expanded = undefined,
    disabled = false,
    class: className = "",
    onclick = undefined,
    children,
  }: Props = $props();

  const classes = $derived(
    [
      "chip",
      `chip--${size}`,
      interactive ? "chip--interactive" : "",
      className,
    ].filter(Boolean).join(" "),
  );
</script>

{#if interactive}
  <button
    type="button"
    class={classes}
    {title}
    aria-expanded={expanded}
    {disabled}
    onclick={onclick}
  >
    {#if children}
      {@render children()}
    {/if}
  </button>
{:else}
  <span class={classes} {title}>
    {#if children}
      {@render children()}
    {/if}
  </span>
{/if}

<style>
  .chip {
    box-sizing: border-box;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 4px;
    padding: 0 8px;
    border-radius: 10px;
    font-size: 11px;
    font-weight: 600;
    line-height: 1;
    letter-spacing: 0.03em;
    text-transform: uppercase;
    vertical-align: middle;
    white-space: nowrap;
  }

  .chip--sm {
    min-height: 20px;
  }

  .chip--md {
    min-height: 22px;
  }

  .chip--interactive {
    appearance: none;
    border: none;
    cursor: pointer;
    font-family: inherit;
    transition: opacity 0.1s;
  }

  .chip--interactive:hover:not(:disabled) {
    opacity: 0.8;
  }

  .chip--interactive:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .chip--green,
  .chip--open {
    background: color-mix(in srgb, var(--accent-green) 15%, transparent);
    color: var(--accent-green);
  }

  .chip--red {
    background: color-mix(in srgb, var(--accent-red) 15%, transparent);
    color: var(--accent-red);
  }

  .chip--amber {
    background: color-mix(in srgb, var(--accent-amber) 15%, transparent);
    color: var(--accent-amber);
  }

  .chip--purple,
  .chip--closed {
    background: color-mix(in srgb, var(--accent-purple) 15%, transparent);
    color: var(--accent-purple);
  }

  .chip--muted {
    background: var(--bg-inset);
    color: var(--text-muted);
  }

  .chip--teal {
    background: color-mix(
      in srgb,
      var(--accent-teal, var(--accent-green)) 15%,
      transparent
    );
    color: var(--accent-teal, var(--accent-green));
  }
</style>
