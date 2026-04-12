<script lang="ts">
  import KanbanBoard
    from "../components/kanban/KanbanBoard.svelte";
</script>

<div class="board-layout">
  <KanbanBoard />
</div>

<style>
  .board-layout {
    flex: 1;
    overflow: hidden;
    background: var(--bg-primary);
    display: flex;
    flex-direction: column;
  }
</style>
