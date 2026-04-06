<script lang="ts">
  import { getStores } from "../context.js";
  import DiffView from "../components/diff/DiffView.svelte";

  const { diff: diffStore } = getStores();

  interface Props {
    owner: string;
    name: string;
    number: number;
  }

  const { owner, name, number }: Props = $props();
</script>

<div class="diff-layout">
  {#key `${owner}/${name}/${number}`}
    <DiffView {owner} {name} {number} />
  {/key}
</div>

<style>
  .diff-layout {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    background: var(--bg-primary);
  }
</style>
