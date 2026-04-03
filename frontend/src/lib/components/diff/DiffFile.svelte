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
    const currentFile = file;
    const currentTheme = theme;
    const currentLang = lang;
    const version = ++tokenVersion;
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
    return tokenCache.get(`${hunkIdx}:${lineIdx}`) ?? [{ content: file.hunks[hunkIdx]!.lines[lineIdx]!.content }];
  }

  function computeCollapsedLines(hunks: DiffHunk[], hunkIdx: number): number {
    if (hunkIdx === 0) return 0;
    const prev = hunks[hunkIdx - 1]!;
    const curr = hunks[hunkIdx]!;
    const prevEndOld = prev.old_start + prev.old_count;
    const gapOld = curr.old_start - prevEndOld;
    return Math.max(gapOld, 0);
  }

  function toggle(): void {
    toggleFileCollapsed(owner, name, number, file.path);
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
      {#if file.additions > 0}
        <span class="stat stat--add">+{file.additions}</span>
      {/if}
      {#if file.deletions > 0}
        <span class="stat stat--del">-{file.deletions}</span>
      {/if}
    </span>
  </button>
  <div class="file-content-accordion" class:file-content-accordion--collapsed={collapsed}>
    <div class="file-content" style:tab-size={tabWidth}>
      {#if file.is_binary}
        <div class="binary-notice">Binary file changed</div>
      {:else}
        {#each file.hunks as hunk, hunkIdx}
          {#if hunkIdx > 0}
            {@const gap = computeCollapsedLines(file.hunks, hunkIdx)}
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

  .file-content-accordion {
    display: grid;
    grid-template-rows: 1fr;
    transition: grid-template-rows 0.2s ease-out;
  }

  .file-content-accordion--collapsed {
    grid-template-rows: 0fr;
  }

  .file-content {
    overflow: hidden;
    min-height: 0;
  }

  .file-content-accordion:not(.file-content-accordion--collapsed) > .file-content {
    overflow-x: auto;
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
