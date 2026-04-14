<script lang="ts">
  import type { Snippet } from "svelte";

  type Tone = "neutral" | "success" | "danger" | "info" | "workflow";
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
    gap: 6px;
    font-size: 13px;
    font-weight: 500;
    padding: 6px 14px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: opacity 0.1s, background 0.1s;
    white-space: nowrap;
    line-height: 1;
  }

  .action-button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .action-button--sm {
    padding: 4px 12px;
    border-radius: 6px;
    font-size: 12px;
  }

  .action-button :global(svg) {
    flex-shrink: 0;
  }

  /* Neutral outline — cancel / secondary */
  .action-button--outline.action-button--neutral {
    background: var(--bg-inset);
    color: var(--text-secondary);
    border: 1px solid var(--border-default);
  }
  .action-button--outline.action-button--neutral:hover:not(:disabled) {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  /* Danger outline — close (neutral at rest, red on hover) */
  .action-button--outline.action-button--danger {
    background: var(--bg-surface);
    color: var(--text-secondary);
    border: 1px solid var(--border-default);
  }
  .action-button--outline.action-button--danger:hover:not(:disabled) {
    background: var(--accent-red, #d73a49);
    color: #fff;
    border-color: var(--accent-red, #d73a49);
  }

  /* Success solid — merge, reopen, confirm */
  .action-button--solid.action-button--success {
    background: #1a7f37;
    color: #e6ffe6;
    border: none;
  }
  .action-button--solid.action-button--success:hover:not(:disabled) {
    background: #176b2e;
  }

  /* Success soft — approve */
  .action-button--soft.action-button--success {
    background: color-mix(in srgb, var(--accent-green) 12%, transparent);
    color: var(--accent-green);
    border: 1px solid color-mix(in srgb, var(--accent-green) 30%, transparent);
  }
  .action-button--soft.action-button--success:hover:not(:disabled) {
    background: color-mix(in srgb, var(--accent-green) 20%, transparent);
  }

  /* Info soft — ready for review */
  .action-button--soft.action-button--info {
    background: color-mix(in srgb, var(--accent-blue) 10%, transparent);
    color: var(--accent-blue);
    border: 1px solid color-mix(in srgb, var(--accent-blue) 30%, transparent);
  }
  .action-button--soft.action-button--info:hover:not(:disabled) {
    background: color-mix(in srgb, var(--accent-blue) 18%, transparent);
  }

  /* Workflow soft — approve workflows */
  .action-button--soft.action-button--workflow {
    background: color-mix(in srgb, var(--accent-purple) 12%, transparent);
    color: var(--accent-purple);
    border: 1px solid color-mix(in srgb, var(--accent-purple) 30%, transparent);
  }
  .action-button--soft.action-button--workflow:hover:not(:disabled) {
    background: color-mix(in srgb, var(--accent-purple) 20%, transparent);
  }
</style>
