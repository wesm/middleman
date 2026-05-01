<script lang="ts">
  import Chip, {
    type ChipTone,
  } from "../shared/Chip.svelte";

  interface Props {
    status: string;
  }
  let { status }: Props = $props();

  const tone: ChipTone = $derived.by(() => {
    switch (status) {
      case "queued":
        return "warning";
      case "running":
        return "info";
      case "done":
        return "success";
      case "failed":
        return "danger";
      default:
        return "muted";
    }
  });
</script>

<Chip
  size="sm"
  {tone}
  dot
  uppercase={false}
  class={`review-status-chip review-status-chip--${status}`}
>
  {status}
</Chip>
