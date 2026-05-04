<script lang="ts">
  import MoreHorizontalIcon from "@lucide/svelte/icons/more-horizontal";
  import { getStores } from "../../context.js";
  import {
    diffFileCategoryOptions,
    type DiffFileCategoryFilter,
  } from "../../utils/diff-categories.js";

  interface Props {
    compact?: boolean;
    showRichPreview?: boolean;
  }

  let { compact = false, showRichPreview = true }: Props = $props();

  const { diff } = getStores();
  const tabOptions = [1, 2, 4, 8] as const;
  const categoryCounts = $derived(diff.getFileCategoryCounts());
  const activeCategory = $derived(diff.getFileCategoryFilter());
  const activeCategoryLabel = $derived(
    diffFileCategoryOptions.find((option) => option.value === activeCategory)?.label ??
      "All",
  );
  let menuOpen = $state(false);
  let compactMenuRef = $state<HTMLDivElement>();

  function setFileCategoryFilter(value: DiffFileCategoryFilter): void {
    diff.setFileCategoryFilter(value);
  }

  function closeCompactMenu(): void {
    menuOpen = false;
  }

  function handleDocumentClick(event: MouseEvent): void {
    if (!menuOpen) return;
    const target = event.target;
    if (target instanceof Node && compactMenuRef?.contains(target)) return;
    closeCompactMenu();
  }

  function handleDocumentKeydown(event: KeyboardEvent): void {
    if (event.key === "Escape") closeCompactMenu();
  }
</script>

<svelte:document onclick={handleDocumentClick} onkeydown={handleDocumentKeydown} />

<div class={["diff-toolbar", compact && "diff-toolbar--compact"]}>
  {#if compact}
    <div class="compact-summary">
      <span class="toolbar-label">Files</span>
      <span class="compact-summary-value">
        {activeCategoryLabel}
        <span class="category-count">({categoryCounts[activeCategory]})</span>
      </span>
      <span class="compact-summary-detail">Tab {diff.getTabWidth()}</span>
    </div>
    <div class="compact-menu-wrap" bind:this={compactMenuRef}>
      <button
        class="compact-more-btn"
        class:compact-more-btn--active={menuOpen}
        type="button"
        aria-label="More diff filters"
        aria-expanded={menuOpen}
        title="More diff filters"
        onclick={() => {
          menuOpen = !menuOpen;
        }}
      >
        <MoreHorizontalIcon size={16} strokeWidth={2} aria-hidden="true" />
      </button>
      {#if menuOpen}
        <div class="compact-menu">
          <div class="compact-menu-section">
            <div class="compact-menu-title">Files</div>
            <div class="compact-menu-grid" role="group" aria-label="Filter changed files">
              {#each diffFileCategoryOptions as option (option.value)}
                <button
                  class="compact-menu-item"
                  class:compact-menu-item--active={activeCategory === option.value}
                  aria-pressed={activeCategory === option.value}
                  type="button"
                  onclick={() => setFileCategoryFilter(option.value)}
                >
                  <span>{option.label}</span>
                  <span class="category-count">({categoryCounts[option.value]})</span>
                </button>
              {/each}
            </div>
          </div>
          <div class="compact-menu-section">
            <div class="compact-menu-title">Tab width</div>
            <div class="compact-menu-grid" role="group" aria-label="Tab width">
              {#each tabOptions as opt (opt)}
                <button
                  class="compact-menu-item"
                  class:compact-menu-item--active={diff.getTabWidth() === opt}
                  aria-pressed={diff.getTabWidth() === opt}
                  type="button"
                  onclick={() => diff.setTabWidth(opt)}
                >
                  {opt}
                </button>
              {/each}
            </div>
          </div>
          <div class="compact-menu-section">
            <button
              class="compact-switch-row"
              type="button"
              role="switch"
              aria-checked={diff.getHideWhitespace()}
              onclick={() => diff.setHideWhitespace(!diff.getHideWhitespace())}
            >
              <span>Hide whitespace</span>
              <span
                class="toggle-switch"
                class:toggle-switch--on={diff.getHideWhitespace()}
                aria-hidden="true"
              >
                <span class="toggle-knob"></span>
              </span>
            </button>
            <button
              class="compact-switch-row"
              type="button"
              role="switch"
              aria-checked={diff.getWordWrap()}
              onclick={() => diff.setWordWrap(!diff.getWordWrap())}
            >
              <span>Word wrap</span>
              <span
                class="toggle-switch"
                class:toggle-switch--on={diff.getWordWrap()}
                aria-hidden="true"
              >
                <span class="toggle-knob"></span>
              </span>
            </button>
            {#if showRichPreview}
              <button
                class="compact-switch-row"
                type="button"
                role="switch"
                aria-checked={diff.getRichPreview()}
                onclick={() => diff.setRichPreview(!diff.getRichPreview())}
              >
                <span>Rich preview</span>
                <span
                  class="toggle-switch"
                  class:toggle-switch--on={diff.getRichPreview()}
                  aria-hidden="true"
                >
                  <span class="toggle-knob"></span>
                </span>
              </button>
            {/if}
          </div>
        </div>
      {/if}
    </div>
  {:else}
    <div class="toolbar-group toolbar-group--category">
      <span class="toolbar-label">Files</span>
      <div class="category-toggle" role="group" aria-label="Filter changed files">
        {#each diffFileCategoryOptions as option (option.value)}
          <button
            class="category-btn"
            class:category-btn--active={activeCategory === option.value}
            aria-pressed={activeCategory === option.value}
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
      {#if showRichPreview}
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
      {/if}
    </div>
  {/if}
</div>

<style>
  .diff-toolbar {
    position: relative;
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 6px 16px;
    background: var(--diff-toolbar-bg);
    border-bottom: 1px solid var(--diff-border);
    flex-shrink: 0;
  }

  .diff-toolbar--compact {
    justify-content: space-between;
    gap: 8px;
    padding: 6px 10px;
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

  .compact-summary {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    color: var(--text-secondary);
    font-size: 11px;
    white-space: nowrap;
  }

  .compact-summary-value {
    color: var(--text-primary);
    font-weight: 600;
  }

  .compact-summary-detail {
    color: var(--text-muted);
  }

  .compact-menu-wrap {
    position: relative;
    flex-shrink: 0;
  }

  .compact-more-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 24px;
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    background: var(--bg-surface);
  }

  .compact-more-btn:hover,
  .compact-more-btn--active {
    color: var(--accent-blue);
    border-color: var(--accent-blue);
  }

  .compact-menu {
    position: absolute;
    z-index: 50;
    top: calc(100% + 4px);
    right: 0;
    width: 224px;
    padding: 6px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    background: var(--bg-surface);
    box-shadow: var(--shadow-md);
  }

  .compact-menu-section {
    padding: 5px 4px;
  }

  .compact-menu-section + .compact-menu-section {
    border-top: 1px solid var(--border-muted);
  }

  .compact-menu-title {
    margin-bottom: 5px;
    color: var(--text-muted);
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  .compact-menu-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 4px;
  }

  .compact-menu-item,
  .compact-switch-row {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    min-height: 26px;
    padding: 4px 6px;
    border: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-secondary);
    font-size: 11px;
    text-align: left;
  }

  .compact-menu-item {
    justify-content: space-between;
  }

  .compact-menu-item:hover,
  .compact-switch-row:hover {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  .compact-menu-item--active {
    background: var(--bg-inset);
    color: var(--text-primary);
    font-weight: 600;
  }

  .compact-switch-row {
    justify-content: space-between;
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
