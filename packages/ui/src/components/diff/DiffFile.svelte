<script lang="ts">
  import { onMount } from "svelte";
  import type { DiffFile as DiffFileType, DiffHunk } from "../../api/types.js";
  import { getStores } from "../../context.js";

  const { diff: diffStore } = getStores();
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

  const collapsed = $derived(diffStore.isFileCollapsed(owner, name, number, file.path));
  const lang = $derived(langFromPath(file.path));

  // Track viewport visibility so off-screen files skip expensive tokenization
  // on whitespace toggles and theme switches.
  let fileEl: HTMLDivElement | undefined = $state();
  let inViewport = $state(true);

  // Local copy of file data, only synced when expanded AND visible. Collapsed
  // or off-screen files keep stale content so whitespace toggles and theme
  // switches don't trigger expensive re-renders and re-tokenization for
  // content no one can see.
  // svelte-ignore state_referenced_locally — synced from file prop via $effect
  let renderedFile = $state(file);

  $effect(() => {
    if (!collapsed && inViewport) {
      renderedFile = file;
    }
  });

  // Use shared theme detection (single MutationObserver for all DiffFile instances).
  let isDark = $state(isDarkTheme());
  onMount(() => {
    const unsubTheme = subscribeTheme((dark) => { isDark = dark; });

    let observer: IntersectionObserver | undefined;
    if (fileEl) {
      observer = new IntersectionObserver(
        (entries) => { inViewport = entries[0]!.isIntersecting; },
        { rootMargin: "200px 0px" },
      );
      observer.observe(fileEl);
    }

    return () => {
      unsubTheme();
      observer?.disconnect();
    };
  });

  // Dual-theme token caches — both themes are computed on load so switching
  // is instant (just read a different cache, zero async work).
  let darkTokens = $state<Map<string, TokenSpan[]>>(new Map());
  let lightTokens = $state<Map<string, TokenSpan[]>>(new Map());
  let tokenVersion = 0;

  const activeTokens = $derived(isDark ? darkTokens : lightTokens);

  // Tokenize in small batches to avoid blocking the main thread.
  const BATCH_SIZE = 50;

  // Tokenize for BOTH themes when file data changes.
  // Skipped for collapsed or off-screen files; runs when they become visible.
  // Does NOT depend on `theme` — theme switches just swap which cache is read.
  $effect(() => {
    const version = ++tokenVersion;
    if (collapsed || !inViewport) return;

    const currentFile = renderedFile;
    const currentLang = lang;
    const newDark = new Map<string, TokenSpan[]>();
    const newLight = new Map<string, TokenSpan[]>();

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
          batch.map(async (item) => {
            const [dark, light] = await Promise.all([
              tokenizeLine(item.content, currentLang, "github-dark"),
              tokenizeLine(item.content, currentLang, "github-light"),
            ]);
            return { key: item.key, dark, light };
          }),
        );
        if (version !== tokenVersion) return;
        for (const r of results) {
          newDark.set(r.key, r.dark);
          newLight.set(r.key, r.light);
        }
        // Update reactively after each batch so lines get highlighted progressively.
        darkTokens = new Map(newDark);
        lightTokens = new Map(newLight);
        // Yield to the browser between batches.
        if (i + BATCH_SIZE < items.length) {
          await new Promise((r) => requestAnimationFrame(r));
        }
      }
    })();
  });

  function getTokens(hunkIdx: number, lineIdx: number): TokenSpan[] {
    return activeTokens.get(`${hunkIdx}:${lineIdx}`) ?? [{ content: renderedFile.hunks[hunkIdx]!.lines[lineIdx]!.content }];
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
    diffStore.toggleFileCollapsed(owner, name, number, file.path);
  }

  function displayPath(f: DiffFileType): string {
    if (f.status === "renamed" && f.old_path !== f.path) {
      return `${f.old_path} -> ${f.path}`;
    }
    return f.path;
  }
</script>

<div class="diff-file" data-file-path={file.path} bind:this={fileEl}>
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
  {#if !collapsed}
    <div class="file-content" style:tab-size={tabWidth}>
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
  {/if}
</div>

<style>
  .diff-file {
    border-top: 2px solid var(--diff-border);
    content-visibility: auto;
    contain-intrinsic-size: auto 500px;
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
