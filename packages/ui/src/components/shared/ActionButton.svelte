<script lang="ts">
  import type { Snippet } from "svelte";

  type Tone =
    | "neutral"
    | "success"
    | "danger"
    | "info"
    | "workflow";
  type Surface = "outline" | "soft" | "solid";
  type Size = "sm" | "md";

  interface Props {
    tone?: Tone;
    surface?: Surface;
    size?: Size;
    type?: "button" | "submit" | "reset";
    disabled?: boolean;
    title?: string;
    label?: string;
    class?: string;
    onclick?: (event: MouseEvent) => void;
    children?: Snippet;
  }

  let {
    tone = "neutral",
    surface = "outline",
    size = "md",
    type = "button",
    disabled = false,
    title = undefined,
    label = undefined,
    class: className = "",
    onclick = undefined,
    children,
  }: Props = $props();

  const classes = $derived(
    [
      "action-button",
      `action-button--${tone}`,
      `action-button--${surface}`,
      `action-button--${size}`,
      className,
    ].filter(Boolean).join(" "),
  );
</script>

<button {type} class={classes} {disabled} {title} onclick={onclick}>
  {#if label}
    <span>{label}</span>
  {/if}
  {#if children}
    {@render children()}
  {/if}
</button>

<style>
  .action-button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--action-button-gap, 8px);
    min-height: var(--action-button-height, 38px);
    padding: 0 var(--action-button-padding-inline, 14px);
    border-radius: var(--action-button-radius, 10px);
    border: 1px solid var(--action-button-border, var(--border-default));
    background: var(--action-button-bg, var(--bg-surface));
    color: var(--action-button-fg, var(--text-primary));
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.08),
      0 1px 2px rgba(0, 0, 0, 0.08);
    font-size: var(--action-button-font-size, 13px);
    font-weight: var(--action-button-font-weight, 600);
    line-height: 1;
    letter-spacing: -0.01em;
    white-space: nowrap;
    transition:
      background 0.12s ease,
      border-color 0.12s ease,
      color 0.12s ease,
      box-shadow 0.12s ease,
      transform 0.12s ease,
      opacity 0.12s ease;
  }

  .action-button:hover:not(:disabled) {
    background: var(--action-button-hover-bg, var(--action-button-bg));
    border-color: var(--action-button-hover-border, var(--action-button-border));
    color: var(--action-button-hover-fg, var(--action-button-fg));
    transform: translateY(-1px);
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.12),
      0 4px 12px rgba(0, 0, 0, 0.12);
  }

  .action-button:focus-visible {
    outline: none;
    box-shadow:
      0 0 0 3px var(--action-button-focus-ring, color-mix(in srgb, var(--accent-blue) 28%, transparent)),
      inset 0 1px 0 rgba(255, 255, 255, 0.12),
      0 1px 2px rgba(0, 0, 0, 0.1);
  }

  .action-button:disabled {
    opacity: 0.55;
    cursor: not-allowed;
    transform: none;
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.04),
      0 1px 2px rgba(0, 0, 0, 0.06);
  }

  .action-button--sm {
    min-height: var(--action-button-height-sm, 30px);
    padding: 0 var(--action-button-padding-inline-sm, 12px);
    border-radius: var(--action-button-radius-sm, 8px);
    font-size: var(--action-button-font-size-sm, 12px);
  }

  .action-button :global(svg) {
    flex-shrink: 0;
  }

  .action-button--outline.action-button--neutral {
    --action-button-bg: var(--action-button-neutral-outline-bg, var(--bg-surface));
    --action-button-border: var(--action-button-neutral-outline-border, var(--border-default));
    --action-button-fg: var(--action-button-neutral-outline-text, var(--text-secondary));
    --action-button-hover-bg: var(--action-button-neutral-outline-hover-bg, var(--bg-surface-hover));
    --action-button-hover-border: var(--action-button-neutral-outline-hover-border, var(--border-default));
    --action-button-hover-fg: var(--action-button-neutral-outline-hover-text, var(--text-primary));
    --action-button-focus-ring: var(--action-button-neutral-focus-ring, color-mix(in srgb, var(--accent-blue) 22%, transparent));
  }

  .action-button--soft.action-button--success {
    --action-button-bg: var(--action-button-success-soft-bg, color-mix(in srgb, var(--accent-green) 14%, transparent));
    --action-button-border: var(--action-button-success-soft-border, color-mix(in srgb, var(--accent-green) 30%, transparent));
    --action-button-fg: var(--action-button-success-soft-text, var(--accent-green));
    --action-button-hover-bg: var(--action-button-success-soft-hover-bg, color-mix(in srgb, var(--accent-green) 22%, transparent));
    --action-button-hover-border: var(--action-button-success-soft-hover-border, color-mix(in srgb, var(--accent-green) 38%, transparent));
    --action-button-hover-fg: var(--action-button-success-soft-hover-text, var(--accent-green));
    --action-button-focus-ring: var(--action-button-success-focus-ring, color-mix(in srgb, var(--accent-green) 24%, transparent));
  }

  .action-button--soft.action-button--danger {
    --action-button-bg: var(--action-button-danger-soft-bg, color-mix(in srgb, var(--accent-red) 12%, transparent));
    --action-button-border: var(--action-button-danger-soft-border, color-mix(in srgb, var(--accent-red) 28%, transparent));
    --action-button-fg: var(--action-button-danger-soft-text, var(--accent-red));
    --action-button-hover-bg: var(--action-button-danger-soft-hover-bg, color-mix(in srgb, var(--accent-red) 18%, transparent));
    --action-button-hover-border: var(--action-button-danger-soft-hover-border, color-mix(in srgb, var(--accent-red) 36%, transparent));
    --action-button-hover-fg: var(--action-button-danger-soft-hover-text, var(--accent-red));
    --action-button-focus-ring: var(--action-button-danger-focus-ring, color-mix(in srgb, var(--accent-red) 24%, transparent));
  }

  .action-button--soft.action-button--info {
    --action-button-bg: var(--action-button-info-soft-bg, color-mix(in srgb, var(--accent-blue) 12%, transparent));
    --action-button-border: var(--action-button-info-soft-border, color-mix(in srgb, var(--accent-blue) 28%, transparent));
    --action-button-fg: var(--action-button-info-soft-text, var(--accent-blue));
    --action-button-hover-bg: var(--action-button-info-soft-hover-bg, color-mix(in srgb, var(--accent-blue) 18%, transparent));
    --action-button-hover-border: var(--action-button-info-soft-hover-border, color-mix(in srgb, var(--accent-blue) 36%, transparent));
    --action-button-hover-fg: var(--action-button-info-soft-hover-text, var(--accent-blue));
    --action-button-focus-ring: var(--action-button-info-focus-ring, color-mix(in srgb, var(--accent-blue) 24%, transparent));
  }

  .action-button--soft.action-button--workflow {
    --action-button-bg: var(--action-button-workflow-soft-bg, color-mix(in srgb, var(--accent-purple) 12%, transparent));
    --action-button-border: var(--action-button-workflow-soft-border, color-mix(in srgb, var(--accent-purple) 28%, transparent));
    --action-button-fg: var(--action-button-workflow-soft-text, var(--accent-purple));
    --action-button-hover-bg: var(--action-button-workflow-soft-hover-bg, color-mix(in srgb, var(--accent-purple) 18%, transparent));
    --action-button-hover-border: var(--action-button-workflow-soft-hover-border, color-mix(in srgb, var(--accent-purple) 36%, transparent));
    --action-button-hover-fg: var(--action-button-workflow-soft-hover-text, var(--accent-purple));
    --action-button-focus-ring: var(--action-button-workflow-focus-ring, color-mix(in srgb, var(--accent-purple) 24%, transparent));
  }

  .action-button--solid.action-button--success {
    --action-button-bg: var(--action-button-success-solid-bg, #1a7f37);
    --action-button-border: var(--action-button-success-solid-border, #1a7f37);
    --action-button-fg: var(--action-button-success-solid-text, #f5fff8);
    --action-button-hover-bg: var(--action-button-success-solid-hover-bg, #176b2e);
    --action-button-hover-border: var(--action-button-success-solid-hover-border, #176b2e);
    --action-button-hover-fg: var(--action-button-success-solid-hover-text, #ffffff);
    --action-button-focus-ring: var(--action-button-success-focus-ring, color-mix(in srgb, var(--accent-green) 24%, transparent));
  }

  .action-button--solid.action-button--danger {
    --action-button-bg: var(--action-button-danger-solid-bg, #c53b2a);
    --action-button-border: var(--action-button-danger-solid-border, #c53b2a);
    --action-button-fg: var(--action-button-danger-solid-text, #fff6f5);
    --action-button-hover-bg: var(--action-button-danger-solid-hover-bg, #a93021);
    --action-button-hover-border: var(--action-button-danger-solid-hover-border, #a93021);
    --action-button-hover-fg: var(--action-button-danger-solid-hover-text, #ffffff);
    --action-button-focus-ring: var(--action-button-danger-focus-ring, color-mix(in srgb, var(--accent-red) 24%, transparent));
  }
</style>
