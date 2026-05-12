<script lang="ts">
  import { providerIcon } from "./provider-icons.ts";

  interface Props {
    provider: string;
    size?: number;
    class?: string;
  }

  let {
    provider,
    size = 16,
    class: className = "",
  }: Props = $props();

  const icon = $derived(providerIcon(provider));
</script>

{#if icon}
  <svg
    class={["provider-icon", className].filter(Boolean).join(" ")}
    role="img"
    aria-label={icon.title}
    viewBox="0 0 24 24"
    width={size}
    height={size}
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
  >
    <title>{icon.title}</title>
    <path d={icon.path}></path>
  </svg>
{/if}

<style>
  .provider-icon {
    display: inline-block;
    flex: 0 0 auto;
    color: currentColor;
    vertical-align: -0.125em;
  }
</style>
