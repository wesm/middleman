<script lang="ts">
  interface Props {
    lineCount: number;
  }

  const { lineCount }: Props = $props();
</script>

<div class="collapsed-region">
  <span class="collapsed-gutter"></span>
  <span class="collapsed-gutter"></span>
  <span class="collapsed-label">
    {lineCount} unchanged {lineCount === 1 ? "line" : "lines"}
  </span>
</div>

<style>
  .collapsed-region {
    display: flex;
    align-items: center;
    border-top: 1px dashed var(--diff-collapsed-border);
    border-bottom: 1px dashed var(--diff-collapsed-border);
    background: var(--diff-collapsed-bg);
    color: var(--diff-line-num);
    font-size: 12px;
    line-height: 20px;
    user-select: none;
  }

  .collapsed-gutter {
    width: 50px;
    flex-shrink: 0;
    background: var(--diff-collapsed-bg);
  }

  .collapsed-label {
    padding: 2px 12px;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--diff-hunk-text);
  }
</style>
