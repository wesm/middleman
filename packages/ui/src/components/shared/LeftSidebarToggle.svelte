<script lang="ts">
  import PanelLeftCloseIcon from "@lucide/svelte/icons/panel-left-close";
  import PanelLeftOpenIcon from "@lucide/svelte/icons/panel-left-open";
  import type { ClassValue } from "svelte/elements";

  type SidebarToggleState = "expanded" | "collapsed";

  interface Props {
    state?: SidebarToggleState;
    label?: string;
    onclick?: ((event: MouseEvent) => void) | undefined;
    class?: ClassValue;
  }

  let {
    state = "expanded",
    label = "sidebar",
    onclick,
    class: className = undefined,
  }: Props = $props();

  const ToggleIcon = $derived(
    state === "collapsed" ? PanelLeftOpenIcon : PanelLeftCloseIcon,
  );
  const action = $derived(
    state === "collapsed" ? "Expand" : "Collapse",
  );
  const accessibleLabel = $derived(`${action} ${label}`);
</script>

<button
  class={[
    "left-sidebar-toggle",
    `left-sidebar-toggle--${state}`,
    className,
  ]}
  {onclick}
  title={accessibleLabel}
  aria-label={accessibleLabel}
  type="button"
>
  <ToggleIcon
    size="14"
    strokeWidth="1.5"
    aria-hidden="true"
  />
</button>

<style>
  .left-sidebar-toggle {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    flex-shrink: 0;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    cursor: pointer;
    transition: color 0.1s, background 0.1s;
  }

  .left-sidebar-toggle:hover {
    color: var(--text-primary);
    background: var(--bg-surface-hover);
  }

  .left-sidebar-toggle--compact {
    width: 22px;
    height: 22px;
  }

  .left-sidebar-toggle--push {
    margin-left: auto;
  }
</style>
