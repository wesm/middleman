<script lang="ts">
  import { getSidebar } from "../context.js";
  import CollapsibleResizableSidebar from "../components/shared/CollapsibleResizableSidebar.svelte";
  import IssueList
    from "../components/sidebar/IssueList.svelte";
  import IssueDetail
    from "../components/detail/IssueDetail.svelte";

  const { isSidebarToggleEnabled, toggleSidebar } = getSidebar();

  interface Props {
    selectedIssue?: {
      owner: string;
      name: string;
      number: number;
    } | null;
    isSidebarCollapsed?: boolean;
    hideSidebar?: boolean;
    sidebarWidth?: number;
    onSidebarResize?: (width: number) => void;
  }

  let {
    selectedIssue = null,
    isSidebarCollapsed = false,
    hideSidebar = false,
    sidebarWidth = 340,
    onSidebarResize,
  }: Props = $props();
</script>

<CollapsibleResizableSidebar
  isCollapsed={isSidebarCollapsed}
  {hideSidebar}
  {sidebarWidth}
  {onSidebarResize}
  showCollapsedStrip={isSidebarToggleEnabled()}
  onExpand={toggleSidebar}
  mainEmpty={selectedIssue === null}
>
  {#snippet sidebar()}
    <IssueList />
  {/snippet}

  {#if selectedIssue !== null}
    <IssueDetail
      owner={selectedIssue.owner}
      name={selectedIssue.name}
      number={selectedIssue.number}
    />
  {:else}
    <div class="placeholder-content">
      <p class="placeholder-text">Select an issue</p>
      <p class="placeholder-hint">j/k to navigate</p>
    </div>
  {/if}
</CollapsibleResizableSidebar>

<style>
  .placeholder-content {
    text-align: center;
  }

  .placeholder-text {
    color: var(--text-muted);
    font-size: 13px;
  }

  .placeholder-hint {
    color: var(--text-muted);
    font-size: 11px;
    margin-top: 8px;
    opacity: 0.7;
  }
</style>
