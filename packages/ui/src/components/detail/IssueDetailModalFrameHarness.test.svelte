<script lang="ts">
  // Test-only harness that exercises the same conditional push/pop
  // pattern that IssueDetail uses for its branch-conflict sub-modal.
  // Mounting IssueDetail directly requires its full provider context;
  // this harness keeps the test focused on the gated-effect wiring.
  import { untrack } from "svelte";
  import { pushModalFrame } from "../../stores/keyboard/modal-stack.svelte.js";

  interface Props {
    open: boolean;
  }

  const { open }: Props = $props();

  $effect(() => {
    if (!open) return;
    return untrack(() => pushModalFrame("issue-detail-confirm", []));
  });
</script>
