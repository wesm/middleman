<script lang="ts">
  import { getStores } from "../../context.js";

  const { diff } = getStores();
  const tabOptions = [1, 2, 4, 8] as const;
</script>

<div class="diff-toolbar">
  <div class="toolbar-group">
    <span class="toolbar-label">Tab width</span>
    <div class="segmented-control">
      {#each tabOptions as opt}
        <button
          class="segment"
          class:segment--active={diff.getTabWidth() === opt}
          onclick={() => diff.setTabWidth(opt)}
        >
          {opt}
        </button>
      {/each}
    </div>
  </div>
  <div class="toolbar-group">
    <span class="toolbar-label">Hide whitespace</span>
    <button
      class="toggle-switch"
      class:toggle-switch--on={diff.getHideWhitespace()}
      role="switch"
      aria-checked={diff.getHideWhitespace()}
      title={diff.getHideWhitespace() ? "Show whitespace changes" : "Hide whitespace changes"}
      onclick={() => diff.setHideWhitespace(!diff.getHideWhitespace())}
    >
      <span class="toggle-knob"></span>
    </button>
  </div>
</div>

<style>
  .diff-toolbar {
    display: flex;
    align-items: center;
    gap: 20px;
    padding: 6px 16px;
    background: var(--diff-toolbar-bg);
    border-bottom: 1px solid var(--diff-border);
    flex-shrink: 0;
  }

  .toolbar-group {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .toolbar-label {
    font-size: 11px;
    color: var(--text-secondary);
    user-select: none;
    white-space: nowrap;
  }

  .segmented-control {
    display: flex;
    border: 1px solid var(--diff-border);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }

  .segment {
    font-size: 11px;
    font-family: var(--font-mono);
    padding: 2px 8px;
    color: var(--text-secondary);
    background: var(--diff-bg);
    border-right: 1px solid var(--diff-border);
    line-height: 18px;
  }

  .segment:last-child {
    border-right: none;
  }

  .segment:hover {
    background: var(--bg-surface-hover);
  }

  .segment--active {
    background: var(--accent-blue);
    color: #ffffff;
  }

  .segment--active:hover {
    background: var(--accent-blue);
  }

  .toggle-switch {
    position: relative;
    width: 36px;
    height: 20px;
    border-radius: 10px;
    background: var(--border-default);
    transition: background 0.2s;
    flex-shrink: 0;
  }

  .toggle-switch--on {
    background: var(--accent-blue);
  }

  .toggle-knob {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: #ffffff;
    transition: transform 0.2s;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.2);
  }

  .toggle-switch--on .toggle-knob {
    transform: translateX(16px);
  }

</style>
