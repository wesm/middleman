<script lang="ts">
  import type { DiffFile } from "../../api/types.js";
  import CommitListSection from "./CommitListSection.svelte";
  import FileTree from "./FileTree.svelte";

  interface Props {
    files: DiffFile[];
    activeFile: string | null;
    whitespaceOnlyCount: number;
    hideWhitespace: boolean;
    onselect: (path: string) => void;
  }

  const { files, activeFile, whitespaceOnlyCount, hideWhitespace, onselect }: Props = $props();

  // Track FileTree's width so the sidebar wrapper stays constrained.
  // Watches both childList changes (collapse/expand swaps root element)
  // and inline style changes (drag-handle resize).
  let treeWrap: HTMLDivElement | undefined = $state();
  let sidebarWidth = $state<number | null>(null);

  function syncWidth(): void {
    if (!treeWrap) return;
    const ft = treeWrap.querySelector<HTMLElement>(".file-tree");
    sidebarWidth = ft ? ft.offsetWidth : null;
  }

  $effect(() => {
    if (!treeWrap) return;
    syncWidth();

    const obs = new MutationObserver(syncWidth);
    // childList: detect collapse/expand toggling the root element
    // subtree + attributes: detect drag-handle style changes on .file-tree
    obs.observe(treeWrap, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ["style"],
    });
    return () => obs.disconnect();
  });
</script>

<div class="diff-sidebar" style:width={sidebarWidth ? `${sidebarWidth}px` : undefined}>
  <CommitListSection />
  <div class="diff-sidebar__tree" bind:this={treeWrap}>
    <FileTree
      {files}
      {activeFile}
      {whitespaceOnlyCount}
      {hideWhitespace}
      {onselect}
    />
  </div>
</div>

<style>
  .diff-sidebar {
    display: flex;
    flex-direction: column;
    overflow: hidden;
    min-height: 0;
    flex-shrink: 0;
  }

  .diff-sidebar__tree {
    flex: 1;
    min-height: 0;
    display: flex;
    overflow: hidden;
  }
</style>
