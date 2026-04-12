<script lang="ts">
  import IssueList
    from "../components/sidebar/IssueList.svelte";
  import IssueDetail
    from "../components/detail/IssueDetail.svelte";

  interface Props {
    selectedIssue?: {
      owner: string;
      name: string;
      number: number;
    } | null;
    isSidebarCollapsed?: boolean;
    hideSidebar?: boolean;
  }

  let {
    selectedIssue = null,
    isSidebarCollapsed = false,
    hideSidebar = false,
  }: Props = $props();
</script>

<div class="list-layout">
  {#if !isSidebarCollapsed}
    <aside class="sidebar">
      <IssueList />
    </aside>
  {:else if !hideSidebar}
    <aside class="sidebar sidebar--collapsed"></aside>
  {/if}
  <section
    class="detail-area"
    class:detail-area--empty={selectedIssue === null}
  >
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
  </section>
</div>

<style>
  .list-layout {
    display: flex;
    flex: 1;
    overflow: hidden;
  }

  .sidebar {
    width: 340px;
    flex-shrink: 0;
    background: var(--bg-surface);
    border-right: 1px solid var(--border-default);
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .sidebar--collapsed {
    width: 0;
    overflow: hidden;
    border-right: none;
  }

  .detail-area {
    flex: 1;
    overflow-y: auto;
    background: var(--bg-primary);
    display: flex;
    flex-direction: column;
  }

  .detail-area--empty {
    align-items: center;
    justify-content: center;
  }

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
