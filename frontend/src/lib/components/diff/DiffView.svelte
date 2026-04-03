<script lang="ts">
  import { onMount } from "svelte";
  import { navigate } from "../../stores/router.svelte.js";
  import {
    getDiff,
    isDiffLoading,
    getDiffError,
    getTabWidth,
    getHideWhitespace,
    getActiveFile,
    setActiveFile,
    isScrolling,
    setScrolling,
    consumeScrollTarget,
    requestScrollToFile,
    loadDiff,
    clearDiff,
  } from "../../stores/diff.svelte.js";
  import { client } from "../../api/runtime.js";
  import FileTree from "./FileTree.svelte";
  import DiffToolbar from "./DiffToolbar.svelte";
  import DiffFileComponent from "./DiffFile.svelte";

  interface Props {
    owner: string;
    name: string;
    number: number;
    inline?: boolean;
  }

  const { owner, name, number, inline = false }: Props = $props();

  let prTitle = $state<string | null>(null);
  let diffArea: HTMLDivElement | undefined = $state();

  // Load diff data on mount. Fetch PR title only in standalone mode.
  onMount(() => {
    void loadDiff(owner, name, number);
    if (!inline) {
      void client
        .GET("/repos/{owner}/{name}/pulls/{number}", {
          params: { path: { owner, name, number } },
        })
        .then(({ data }) => {
          if (data) {
            prTitle = data.pull_request.Title;
          }
        });
    }

    return () => {
      clearDiff();
    };
  });

  const diff = $derived(getDiff());
  const loading = $derived(isDiffLoading());
  const error = $derived(getDiffError());
  const tabWidth = $derived(getTabWidth());
  const hideWhitespace = $derived(getHideWhitespace());

  const totalAdditions = $derived(
    diff?.files.reduce((sum, f) => sum + f.additions, 0) ?? 0,
  );
  const totalDeletions = $derived(
    diff?.files.reduce((sum, f) => sum + f.deletions, 0) ?? 0,
  );

  function goBack(): void {
    if (history.state?.fromApp) {
      history.back();
    } else {
      navigate(`/pulls/${owner}/${name}/${number}`);
    }
  }

  function scrollToFile(path: string): void {
    if (!diffArea) return;
    const el = diffArea.querySelector(`[data-file-path="${CSS.escape(path)}"]`);
    if (el) {
      el.scrollIntoView({ behavior: "smooth", block: "start" });
    }
  }

  // Watch for scroll requests from the sidebar file tree (via the store).
  $effect(() => {
    const target = consumeScrollTarget();
    if (target) {
      // Use a microtask to ensure the DOM has settled.
      queueMicrotask(() => scrollToFile(target));
    }
  });

  // Scroll-based active file tracking.
  // Suppressed during programmatic smooth scrolls to prevent flashing/overshoot.
  let scrollFallbackTimer: ReturnType<typeof setTimeout> | null = null;

  function onDiffScroll(): void {
    if (!diffArea || !diff) return;
    // During programmatic smooth scroll, skip updating activeFile to prevent
    // intermediate files from flashing in the sidebar.
    if (isScrolling()) {
      // Fallback: clear scrolling flag if scrollend doesn't fire (e.g. older browsers).
      if (scrollFallbackTimer) clearTimeout(scrollFallbackTimer);
      scrollFallbackTimer = setTimeout(() => { setScrolling(false); }, 200);
      return;
    }
    const rect = diffArea.getBoundingClientRect();
    const threshold = rect.top + 60;

    let current: string | null = null;
    for (const file of diff.files) {
      const el = diffArea.querySelector(`[data-file-path="${CSS.escape(file.path)}"]`);
      if (!el) continue;
      const elRect = el.getBoundingClientRect();
      if (elRect.top <= threshold) {
        current = file.path;
      }
    }
    if (current !== null) {
      setActiveFile(current);
    }
  }

  function onDiffScrollEnd(): void {
    if (scrollFallbackTimer) { clearTimeout(scrollFallbackTimer); scrollFallbackTimer = null; }
    setScrolling(false);
  }

  // j/k keyboard navigation between files.
  function handleKeydown(e: KeyboardEvent): void {
    const tag = (e.target as HTMLElement).tagName;
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
    if (!diff || diff.files.length === 0) return;

    if (e.key === "j" || e.key === "k") {
      e.preventDefault();
      const paths = diff.files.map((f) => f.path);
      const currentIdx = getActiveFile() ? paths.indexOf(getActiveFile()!) : -1;
      let nextIdx: number;
      if (e.key === "j") {
        nextIdx = currentIdx < paths.length - 1 ? currentIdx + 1 : currentIdx;
      } else {
        nextIdx = currentIdx > 0 ? currentIdx - 1 : 0;
      }
      const nextPath = paths[nextIdx] ?? null;
      if (nextPath) requestScrollToFile(nextPath);
    }
  }

  $effect(() => {
    window.addEventListener("keydown", handleKeydown);
    return () => window.removeEventListener("keydown", handleKeydown);
  });
</script>

<div class="diff-view">
  {#if !inline}
    <!-- Top bar (standalone mode only) -->
    <div class="diff-topbar">
      <button class="back-btn" onclick={goBack}>
        <span class="back-arrow">&#8592;</span>
        Back
      </button>
      <div class="topbar-info">
        {#if prTitle}
          <span class="topbar-title">{prTitle}</span>
        {/if}
        {#if diff}
          <span class="topbar-stats">
            {diff.files.length} {diff.files.length === 1 ? "file" : "files"}
            <span class="topbar-stat topbar-stat--add">+{totalAdditions}</span>
            <span class="topbar-stat topbar-stat--del">-{totalDeletions}</span>
          </span>
        {/if}
      </div>
    </div>
  {/if}

  {#if diff?.stale}
    <div class="stale-banner">
      Diff may be outdated -- showing changes as of an earlier version of this PR.
    </div>
  {/if}

  <div class="diff-body">
    {#if loading && !diff}
      <div class="diff-state">
        <p class="diff-state-msg">Loading diff...</p>
      </div>
    {:else if error}
      <div class="diff-state">
        <p class="diff-state-msg diff-state-msg--error">{error}</p>
      </div>
    {:else if diff}
      {#if !inline}
        <FileTree
          files={diff.files}
          activeFile={getActiveFile()}
          whitespaceOnlyCount={diff.whitespace_only_count}
          {hideWhitespace}
          onselect={requestScrollToFile}
        />
      {/if}
      <div class="diff-main">
        <DiffToolbar />
        <div
          class="diff-area"
          bind:this={diffArea}
          onscroll={onDiffScroll}
          onscrollend={onDiffScrollEnd}
        >
          {#each diff.files as file (file.path)}
            <DiffFileComponent
              {file}
              {owner}
              {name}
              {number}
              {tabWidth}
            />
          {/each}
        </div>
      </div>
    {/if}
  </div>
</div>

<style>
  .diff-view {
    display: flex;
    flex-direction: column;
    flex: 1;
    overflow: hidden;
    background: var(--diff-bg);
  }

  .diff-topbar {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 8px 16px;
    background: var(--diff-header-bg);
    border-bottom: 1px solid var(--diff-border);
    flex-shrink: 0;
  }

  .back-btn {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    color: var(--text-secondary);
    padding: 4px 8px;
    border-radius: var(--radius-sm);
    flex-shrink: 0;
  }

  .back-btn:hover {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  .back-arrow {
    font-size: 14px;
  }

  .topbar-info {
    display: flex;
    align-items: center;
    gap: 12px;
    flex: 1;
    min-width: 0;
  }

  .topbar-title {
    font-size: 13px;
    font-weight: 500;
    color: var(--diff-text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .topbar-stats {
    font-size: 12px;
    color: var(--text-secondary);
    flex-shrink: 0;
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .topbar-stat {
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 600;
  }

  .topbar-stat--add {
    color: var(--diff-add-text);
  }

  .topbar-stat--del {
    color: var(--diff-del-text);
  }

  .stale-banner {
    padding: 6px 16px;
    background: var(--diff-stale-bg);
    color: var(--diff-stale-text);
    border-bottom: 1px solid var(--diff-stale-border);
    font-size: 12px;
    flex-shrink: 0;
  }

  .diff-body {
    display: flex;
    flex: 1;
    overflow: hidden;
  }

  .diff-main {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
    overflow: hidden;
  }

  .diff-area {
    flex: 1;
    overflow: auto;
  }

  .diff-state {
    display: flex;
    align-items: center;
    justify-content: center;
    flex: 1;
  }

  .diff-state-msg {
    font-size: 13px;
    color: var(--text-muted);
  }

  .diff-state-msg--error {
    color: var(--accent-red);
  }
</style>
