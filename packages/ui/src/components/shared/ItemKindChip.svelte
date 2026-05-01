<script lang="ts">
  import Chip, {
    type ChipSize,
    type ChipTone,
  } from "./Chip.svelte";

  interface Props {
    kind: string;
    size?: ChipSize;
    title?: string;
    class?: string;
  }

  const {
    kind,
    size = "xs",
    title = undefined,
    class: className = "",
  }: Props = $props();

  const normalized = $derived(kind.toLowerCase());
  const label = $derived(normalized === "pr" ? "PR" : "Issue");
  const tone: ChipTone = $derived(
    normalized === "pr" ? "info" : "merged",
  );
  const chipClass = $derived(
    ["chip--kind", `chip--kind-${normalized}`, className]
      .filter(Boolean)
      .join(" "),
  );
</script>

<Chip {size} {tone} {title} class={chipClass}>
  {label}
</Chip>
