<script lang="ts">
  import { onMount } from "svelte";
  import type { DiffFile as DiffFileType, DiffHunk } from "../../api/types.js";
  import { isFileCollapsed, toggleFileCollapsed } from "../../stores/diff.svelte.js";
  import { tokenizeLine, langFromPath, isDarkTheme, subscribeTheme, type TokenSpan } from "../../utils/highlight.js";
  import DiffLineComponent from "./DiffLine.svelte";
  import CollapsedRegion from "./CollapsedRegion.svelte";

  interface Props {
    file: DiffFileType;
    owner: string;
    name: string;
    number: number;
    tabWidth: number;
  }

  const { file, owner, name, number, tabWidth }: Props = $props();

  const collapsed = $derived(isFileCollapsed(owner, name, number, file.path));
  const lang = $derived(langFromPath(file.path));

  // Local copy of file data, only synced when expanded. Collapsed files keep
  // stale content so whitespace toggles don't trigger expensive re-renders
  // and re-tokenization for hidden content.
  // svelte-ignore state_referenced_locally — synced from file prop via $effect when expanded
  let renderedFile = $state(file);

  $effect(() => {
    if (!collapsed) {
      renderedFile = file;
    }
  });

  // Use shared theme detection (single MutationObserver for all DiffFile instances).
  let isDark = $state(isDarkTheme());
  onMount(() => subscribeTheme((dark) => { isDark = dark; }));

  const theme = $derived(isDark ? "github-dark" as const : "github-light" as const);

  // Syntax-highlighted tokens cache.
  // Map from "hunkIdx:lineIdx" -> TokenSpan[]
  let tokenCache = $state<Map<string, TokenSpan[]>>(new Map());
  let tokenVersion = 0;

  // Tokenize in small batches to avoid blocking the main thread.
  const BATCH_SIZE = 50;

  // Recompute highlights when file or theme changes.
  $effect(() => {
    const version = ++tokenVersion;
    if (collapsed) return;

    const currentFile = renderedFile;
    const currentTheme = theme;
    const currentLang = lang;
    const newCache = new Map<string, TokenSpan[]>();

    void (async () => {
      const items: Array<{ key: string; content: string }> = [];
      for (let hi = 0; hi < currentFile.hunks.length; hi++) {
        const hunk = currentFile.hunks[hi]!;
        for (let li = 0; li < hunk.lines.length; li++) {
          items.push({ key: `${hi}:${li}`, content: hunk.lines[li]!.content });
        }
      }

      for (let i = 0; i < items.length; i += BATCH_SIZE) {
        if (version !== tokenVersion) return;
        const batch = items.slice(i, i + BATCH_SIZE);
        const results = await Promise.all(
          batch.map(async (item) => ({
            key: item.key,
            spans: await tokenizeLine(item.content, currentLang, currentTheme),
          })),
        );
        if (version !== tokenVersion) return;
        for (const r of results) {
          newCache.set(r.key, r.spans);
        }
        // Update reactively after each batch so lines get highlighted progressively.
        tokenCache = new Map(newCache);
        // Yield to the browser between batches.
        if (i + BATCH_SIZE < items.length) {
          await new Promise((r) => requestAnimationFrame(r));
        }
      }
    })();
  });

  function getTokens(hunkIdx: number, lineIdx: number): TokenSpan[] {
    return tokenCache.get(`${hunkIdx}:${lineIdx}`) ?? [{ content: renderedFile.hunks[hunkIdx]!.lines[lineIdx]!.content }];
  }

  function computeCollapsedLines(hunks: DiffHunk[], hunkIdx: number): number {
    if (hunkIdx === 0) return 0;
    const prev = hunks[hunkIdx - 1]!;
    const curr = hunks[hunkIdx]!;
    const prevEndOld = prev.old_start + prev.old_count;
    const gapOld = curr.old_start - prevEndOld;
    return Math.max(gapOld, 0);
  }

  let contentEl: HTMLDivElement | undefined = $state();
  let animating = $state(false);

  function toggle(): void {
    if (!contentEl) {
      toggleFileCollapsed(owner, name, number, file.path);
      return;
    }

    const isExpanded = !collapsed;
    const scrollH = contentEl.scrollHeight;
    const animateH = Math.min(scrollH, window.innerHeight);
    const ms = Math.min(Math.max(Math.round(animateH / 3), 150), 500);

    animating = true;

    if (isExpanded) {
      // Collapse: snap off-screen content away instantly, then animate visible portion to 0.
      const currentH = contentEl.getBoundingClientRect().height;
      contentEl.style.transition = 'none';
      contentEl.style.overflow = 'hidden';
      contentEl.style.maxHeight = `${Math.min(currentH, animateH)}px`;
      contentEl.offsetHeight;
      contentEl.style.transition = `max-height ${ms}ms ease-out`;
      contentEl.style.maxHeight = '0px';
    } else {
      // Expand: animate from current to viewport height, then snap to full.
      const currentH = contentEl.getBoundingClientRect().height;
      contentEl.style.transition = 'none';
      contentEl.style.overflow = 'hidden';
      contentEl.style.maxHeight = `${currentH}px`;
      contentEl.offsetHeight;
      contentEl.style.transition = `max-height ${ms}ms ease-out`;
      contentEl.style.maxHeight = `${animateH}px`;
    }

    toggleFileCollapsed(owner, name, number, file.path);
  }

  function onTransitionEnd(e: TransitionEvent): void {
    if (e.target !== contentEl || e.propertyName !== 'max-height') return;
    animating = false;
    contentEl.style.transition = '';
    contentEl.style.maxHeight = '';
    if (!collapsed) {
      contentEl.style.overflow = '';
    }
  }

  function displayPath(f: DiffFileType): string {
    if (f.status === "renamed" && f.old_path !== f.path) {
      return `${f.old_path} -> ${f.path}`;
    }
    return f.path;
  }
</script>

<div class="diff-file" data-file-path={file.path}>
  <button class="file-header" onclick={toggle} title={collapsed ? "Expand file" : "Collapse file"}>
    <svg class="collapse-chevron" class:collapse-chevron--collapsed={collapsed} width="12" height="12" viewBox="0 0 12 12" fill="none">
      <path d="M3 4.5L6 7.5L9 4.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
    </svg>
    <span class="file-path" class:file-path--deleted={file.status === "deleted"}>
      {displayPath(file)}
    </span>
    <span class="file-stats">
      <span class="stat" class:stat--add={file.additions > 0} class:stat--dim={file.additions === 0}>+{file.additions}</span>
      <span class="stat" class:stat--del={file.deletions > 0} class:stat--dim={file.deletions === 0}>-{file.deletions}</span>
    </span>
  </button>
  <div
    class="file-content"
    class:file-content--collapsed={collapsed && !animating}
    bind:this={contentEl}
    style:tab-size={tabWidth}
    ontransitionend={onTransitionEnd}
  >
      {#if renderedFile.is_binary}
        <div class="binary-notice">Binary file changed</div>
      {:else}
        {#each renderedFile.hunks as hunk, hunkIdx}
          {#if hunkIdx > 0}
            {@const gap = computeCollapsedLines(renderedFile.hunks, hunkIdx)}
            {#if gap > 0}
              <CollapsedRegion lineCount={gap} />
            {/if}
          {/if}
          <div class="hunk-header">
            <span class="hunk-gutter"></span>
            <span class="hunk-gutter"></span>
            <span class="hunk-text">@@ -{hunk.old_start},{hunk.old_count} +{hunk.new_start},{hunk.new_count} @@{hunk.section ? ` ${hunk.section}` : ""}</span>
          </div>
          {#each hunk.lines as line, lineIdx}
            <DiffLineComponent
              type={line.type}
              content={line.content}
              {...(line.old_num != null ? { oldNum: line.old_num } : {})}
              {...(line.new_num != null ? { newNum: line.new_num } : {})}
              {...(line.no_newline ? { noNewline: line.no_newline } : {})}
              tokens={getTokens(hunkIdx, lineIdx)}
            />
          {/each}
        {/each}
      {/if}
  </div>
</div>

<style>
  .diff-file {
    border-top: 2px solid var(--diff-border);
  }

  .file-header {
    position: sticky;
    top: 0;
    z-index: 2;
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 6px 12px;
    background: var(--diff-header-bg);
    border-bottom: 1px solid var(--diff-border);
    font-size: 12px;
    text-align: left;
    cursor: pointer;
    color: var(--diff-text);
  }

  .file-header:hover {
    background: var(--bg-surface-hover);
  }

  .collapse-chevron {
    transition: transform 0.15s ease-out;
    flex-shrink: 0;
  }

  .collapse-chevron--collapsed {
    transform: rotate(-90deg);
  }

  .file-path {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--diff-text);
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .file-path--deleted {
    text-decoration: line-through;
  }

  .file-stats {
    display: flex;
    gap: 6px;
    flex-shrink: 0;
  }

  .stat {
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 600;
    min-width: 3.5ch;
    text-align: right;
  }

  .stat--add {
    color: var(--diff-add-text);
  }

  .stat--del {
    color: var(--diff-del-text);
  }

  .stat--dim {
    opacity: 0.3;
  }

  .file-content {
    min-height: 0;
    overflow-x: auto;
  }

  .file-content--collapsed {
    max-height: 0;
    overflow: hidden;
  }

  .binary-notice {
    padding: 20px;
    text-align: center;
    color: var(--diff-line-num);
    font-size: 13px;
    font-style: italic;
  }

  .hunk-header {
    display: flex;
    align-items: stretch;
    background: var(--diff-hunk-bg);
    color: var(--diff-hunk-text);
    font-family: var(--font-mono);
    font-size: 11px;
    line-height: 20px;
  }

  .hunk-gutter {
    width: 50px;
    flex-shrink: 0;
    background: var(--diff-hunk-bg);
  }

  .hunk-text {
    padding: 2px 12px;
    white-space: pre;
  }
</style>
