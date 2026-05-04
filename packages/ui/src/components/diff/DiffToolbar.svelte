<script lang="ts">
  import { getStores } from "../../context.js";
  import {
    diffFileCategoryOptions,
    type DiffFileCategoryFilter,
  } from "../../utils/diff-categories.js";

  const { diff } = getStores();
  const tabOptions = [1, 2, 4, 8] as const;
  const categoryCounts = $derived(diff.getFileCategoryCounts());

  function setFileCategoryFilter(value: DiffFileCategoryFilter): void {
    diff.setFileCategoryFilter(value);
  }
</script>

<div class="diff-toolbar">
  <div class="toolbar-group toolbar-group--category">
    <span class="toolbar-label">Files</span>
    <div class="category-toggle" role="group" aria-label="Filter changed files">
      {#each diffFileCategoryOptions as option (option.value)}
        <button
          class="category-btn"
          class:category-btn--active={diff.getFileCategoryFilter() === option.value}
          aria-pressed={diff.getFileCategoryFilter() === option.value}
          onclick={() => setFileCategoryFilter(option.value)}
        >
          <span>{option.label}</span> <span class="category-count">({categoryCounts[option.value]})</span>
        </button>
      {/each}
    </div>
  </div>
  <div class="toolbar-settings">
    <div class="toolbar-group">
      <span class="toolbar-label">Tab width</span>
      <div class="segmented-control">
        {#each tabOptions as opt (opt)}
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
    <div class="toolbar-group">
      <span class="toolbar-label">Word wrap</span>
      <button
        class="toggle-switch"
        class:toggle-switch--on={diff.getWordWrap()}
        role="switch"
        aria-label="Word wrap"
        aria-checked={diff.getWordWrap()}
        title={diff.getWordWrap() ? "Disable word wrap" : "Enable word wrap"}
        onclick={() => diff.setWordWrap(!diff.getWordWrap())}
      >
        <span class="toggle-knob"></span>
      </button>
    </div>
    <div class="toolbar-group">
      <span class="toolbar-label">Rich preview</span>
      <button
        class="toggle-switch"
        class:toggle-switch--on={diff.getRichPreview()}
        role="switch"
        aria-label="Rich preview"
        aria-checked={diff.getRichPreview()}
        title={diff.getRichPreview() ? "Show unified diff" : "Show rich file previews"}
        onclick={() => diff.setRichPreview(!diff.getRichPreview())}
      >
        <span class="toggle-knob"></span>
      </button>
    </div>
  </div>
</div>

<style>
  .diff-toolbar {
    display: flex;
    align-items: center;
    gap: 16px;
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

  .toolbar-group--category {
    min-width: 0;
  }

  .toolbar-settings {
    display: flex;
    align-items: center;
    gap: 20px;
    margin-left: auto;
    flex-shrink: 0;
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

  .category-toggle {
    display: flex;
    gap: 2px;
    min-width: 0;
    padding: 2px;
    background: var(--bg-inset);
    border-radius: 6px;
  }

  .category-btn {
    min-width: 56px;
    padding: 2px 8px;
    border: none;
    border-radius: 4px;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 11px;
    font-weight: 500;
    white-space: nowrap;
  }

  .category-btn:hover {
    color: var(--text-primary);
  }

  .category-btn--active {
    background: var(--bg-surface);
    color: var(--text-primary);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.1);
  }

  .category-count {
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
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

  @media (max-width: 760px) {
    .diff-toolbar {
      align-items: flex-start;
      flex-direction: column;
    }

    .toolbar-settings {
      margin-left: 0;
      flex-wrap: wrap;
    }
  }
</style>
