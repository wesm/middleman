<script lang="ts">
  import { tick, untrack } from "svelte";

  import { pushModalFrame } from "@middleman/ui/stores/keyboard/modal-stack";
  import type { ModalFrameAction } from "@middleman/ui/stores/keyboard/keyspec";
  import { getStores, ItemStateChip } from "@middleman/ui";
  import { timeAgo } from "@middleman/ui/utils/time";
  import type { Issue, PullRequest } from "@middleman/ui/api/types";
  import { buildIssueRoute, buildPullRequestRoute } from "@middleman/ui/routes";
  import {
    closePalette,
    isPaletteOpen,
  } from "../../stores/keyboard/palette-state.svelte.js";
  import { buildContext } from "../../stores/keyboard/context.svelte.js";
  import { getAllActions } from "../../stores/keyboard/registry.svelte.js";
  import {
    groupResults,
    parsePaletteQuery,
  } from "../../stores/keyboard/palette-search.svelte.js";
  import { navigate } from "../../stores/router.svelte.js";
  import type { Action } from "../../stores/keyboard/types.js";

  // getStores() returns undefined when the palette is mounted outside the
  // <Provider> context (notably the unit-test fixture in
  // Palette.svelte.test.ts). In that case the search inputs collapse to empty
  // arrays and the palette still renders so downstream tests can drive the
  // shell without setting up a full app context.
  const stores = getStores() as
    | ReturnType<typeof getStores>
    | undefined;

  let dialogEl: HTMLDivElement | undefined = $state();
  let inputEl: HTMLInputElement | undefined = $state();
  let query = $state("");
  let highlightIndex = $state(0);

  const parsed = $derived(parsePaletteQuery(query));
  const visibleCommands = $derived.by(() => {
    if (!stores) {
      // Test-fixture path: with no Provider there is no Context to evaluate
      // `when` predicates against, so surface every registered action so the
      // unit tests can drive preview/highlight behavior without standing up
      // the full app context. Production callers always provide stores.
      return getAllActions();
    }
    const ctx = buildContext(stores);
    return getAllActions().filter((a) => a.when(ctx));
  });
  const grouped = $derived(
    groupResults({
      commands: visibleCommands,
      pulls: stores ? stores.pulls.getPulls() : [],
      issues: stores ? stores.issues.getIssues() : [],
      parsed,
    }),
  );
  const hasResults = $derived(
    grouped.commands.length + grouped.pulls.length + grouped.issues.length > 0,
  );

  type FlatResult =
    | { kind: "command"; item: Action }
    | { kind: "pull"; item: PullRequest }
    | { kind: "issue"; item: Issue };

  const flatResults = $derived<FlatResult[]>([
    ...grouped.commands.map<FlatResult>((c) => ({ kind: "command", item: c })),
    ...grouped.pulls.map<FlatResult>((p) => ({ kind: "pull", item: p })),
    ...grouped.issues.map<FlatResult>((i) => ({ kind: "issue", item: i })),
  ]);

  // Reset the highlight to the top whenever the query changes. The first match
  // is the assumed pick, and keeping a stale offset across keystrokes makes
  // the preview jump around as the result list rebuilds.
  $effect(() => {
    void query;
    untrack(() => {
      highlightIndex = 0;
    });
  });

  // Clamp highlightIndex back into range when the result list shrinks. When
  // empty, leave it at 0 — `highlighted` will still be null.
  $effect(() => {
    const n = flatResults.length;
    if (n === 0) return;
    if (highlightIndex >= n) {
      untrack(() => {
        highlightIndex = n - 1;
      });
    } else if (highlightIndex < 0) {
      untrack(() => {
        highlightIndex = 0;
      });
    }
  });

  const highlighted = $derived<FlatResult | null>(
    flatResults[highlightIndex] ?? null,
  );

  function pullKey(repoOwner: string, repoName: string, num: number): string {
    return `${repoOwner}/${repoName}#${num}`;
  }

  function runHighlighted(): void {
    const result = highlighted;
    if (result === null) return;
    if (result.kind === "command") {
      // Close before invoking the handler so navigation-style commands
      // (e.g. nav.settings) don't race the modal teardown — the route
      // change can unmount the palette host while the handler runs.
      const action = result.item;
      const ctxStores = stores;
      closePalette();
      // The unit-test fixture mounts the palette without a Provider, so
      // `stores` is undefined and `buildContext` cannot run. Hand the
      // handler an empty context object in that case — production
      // actions all read `stores()` via their own getter, so the only
      // handlers that actually invoke through the test fixture are the
      // simple ones the unit tests register.
      const ctx = ctxStores
        ? buildContext(ctxStores)
        : ({} as ReturnType<typeof buildContext>);
      try {
        untrack(() => action.handler(ctx));
      } catch (err) {
        // Mirror dispatch.svelte.ts/runHandler: log and keep the palette
        // host alive so a throwing handler doesn't crash the app.
        console.error(`palette action ${action.id} failed`, err);
      }
      return;
    }
    if (result.kind === "pull") {
      const pr = result.item;
      closePalette();
      navigate(
        buildPullRequestRoute({
          provider: pr.repo.provider,
          platformHost: pr.repo.platform_host,
          owner: pr.repo.owner,
          name: pr.repo.name,
          repoPath: pr.repo.repo_path,
          number: pr.Number,
        }),
      );
      return;
    }
    const issue = result.item;
    closePalette();
    navigate(
      buildIssueRoute({
        provider: issue.repo.provider,
        platformHost: issue.repo.platform_host,
        owner: issue.repo.owner,
        name: issue.repo.name,
        repoPath: issue.repo.repo_path,
        number: issue.Number,
      }),
    );
  }

  function selectRowAt(index: number): void {
    highlightIndex = index;
    runHighlighted();
  }

  function bodyExcerpt(body: string | undefined): string {
    if (!body) return "";
    return body.slice(0, 200);
  }

  function onPaletteKeyDown(e: KeyboardEvent): void {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      const last = flatResults.length - 1;
      if (last < 0) return;
      highlightIndex = Math.min(last, highlightIndex + 1);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      highlightIndex = Math.max(0, highlightIndex - 1);
    } else if (e.key === "Enter") {
      e.preventDefault();
      runHighlighted();
    }
  }

  $effect(() => {
    if (!isPaletteOpen()) return;
    const closeAction: ModalFrameAction = {
      id: "palette.close",
      label: "Close palette",
      binding: [
        { key: "Escape" },
        { key: "k", ctrlOrMeta: true },
        { key: "p", ctrlOrMeta: true },
      ],
      priority: 100,
      when: () => true,
      handler: () => closePalette(),
    };
    const cleanup = untrack(() => pushModalFrame("palette", [closeAction]));
    // Move keyboard focus into the search input on open. Without this the
    // user's existing focus stays on whatever was active before, so typed
    // characters land in the wrong field.
    void tick().then(() => inputEl?.focus());
    return cleanup;
  });

  // Focus trap: keep Tab / Shift+Tab cycling within the palette dialog so
  // focus never escapes to the page underneath while the palette is open.
  // Initial focus is handled by the effect above via tick(); this trap only
  // intercepts subsequent Tab navigation.
  $effect(() => {
    if (!isPaletteOpen() || !dialogEl) return;
    const focusable = (): HTMLElement[] =>
      Array.from(
        dialogEl!.querySelectorAll<HTMLElement>(
          "input, button, [tabindex]:not([tabindex='-1'])",
        ),
      ).filter((e) => !e.hasAttribute("disabled"));
    function trap(e: KeyboardEvent): void {
      if (e.key !== "Tab") return;
      const els = focusable();
      if (els.length === 0) return;
      const first = els[0]!;
      const last = els[els.length - 1]!;
      if (e.shiftKey && document.activeElement === first) {
        last.focus();
        e.preventDefault();
      } else if (!e.shiftKey && document.activeElement === last) {
        first.focus();
        e.preventDefault();
      }
    }
    // Capture dialogEl into a local before registering so the cleanup
    // detaches from the same node we attached to, even if dialogEl is
    // reassigned or unmounted before cleanup runs.
    const el = dialogEl;
    el.addEventListener("keydown", trap);
    return () => el.removeEventListener("keydown", trap);
  });
</script>

{#if isPaletteOpen()}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="palette-backdrop" onclick={closePalette}></div>
  <div
    bind:this={dialogEl}
    class="palette"
    role="dialog"
    aria-modal="true"
    aria-label="Command palette"
  >
    <input
      bind:this={inputEl}
      bind:value={query}
      onkeydown={onPaletteKeyDown}
      class="palette-input"
      placeholder="Search loaded PRs, issues, commands..."
    />
    <div class="palette-body">
      <div class="palette-list">
        {#if parsed.scope === "reserved"}
          <div class="palette-row palette-row-disabled">
            Repo and workspace search land in v2. Try
            <code>pr:</code>
            or
            <code>issue:</code>
            instead.
          </div>
        {:else if !hasResults}
          <div class="palette-row palette-row-disabled">No results</div>
        {:else}
          {#if grouped.commands.length > 0}
            <div class="palette-group">
              <div class="palette-group-header">Commands</div>
              {#each grouped.commands as command, ci (command.id)}
                {@const flatIdx = ci}
                <button
                  class="palette-row {flatIdx === highlightIndex
                    ? 'palette-row-highlight'
                    : ''}"
                  type="button"
                  onclick={() => selectRowAt(flatIdx)}
                >
                  <span class="palette-row-label">{command.label}</span>
                  <span class="palette-row-tag">{command.id}</span>
                </button>
              {/each}
            </div>
          {/if}
          {#if grouped.pulls.length > 0}
            <div class="palette-group">
              <div class="palette-group-header">Pull requests</div>
              {#each grouped.pulls as pr, pi (pullKey(pr.repo_owner, pr.repo_name, pr.Number))}
                {@const flatIdx = grouped.commands.length + pi}
                <button
                  class="palette-row {flatIdx === highlightIndex
                    ? 'palette-row-highlight'
                    : ''}"
                  type="button"
                  onclick={() => selectRowAt(flatIdx)}
                >
                  <span class="palette-row-tag">
                    {pr.repo_owner}/{pr.repo_name} #{pr.Number}
                  </span>
                  <span class="palette-row-label">{pr.Title}</span>
                </button>
              {/each}
            </div>
          {/if}
          {#if grouped.issues.length > 0}
            <div class="palette-group">
              <div class="palette-group-header">Issues</div>
              {#each grouped.issues as issue, ii (pullKey(issue.repo_owner, issue.repo_name, issue.Number))}
                {@const flatIdx =
                  grouped.commands.length + grouped.pulls.length + ii}
                <button
                  class="palette-row {flatIdx === highlightIndex
                    ? 'palette-row-highlight'
                    : ''}"
                  type="button"
                  onclick={() => selectRowAt(flatIdx)}
                >
                  <span class="palette-row-tag">
                    {issue.repo_owner}/{issue.repo_name} #{issue.Number}
                  </span>
                  <span class="palette-row-label">{issue.Title}</span>
                </button>
              {/each}
            </div>
          {/if}
        {/if}
      </div>
      <div class="palette-preview">
        {#if highlighted === null}
          <div class="preview-empty">Highlight a result to preview it</div>
        {:else if highlighted.kind === "command"}
          {@const action = highlighted.item}
          <div class="preview-title">{action.label}</div>
          <div class="preview-subtitle">Scope: {action.scope}</div>
          <div class="preview-meta">Available when: context-conditional</div>
        {:else if highlighted.kind === "pull"}
          {@const pr = highlighted.item}
          <div class="preview-header">
            <div class="preview-title">{pr.Title}</div>
            <ItemStateChip
              state={(pr.IsDraft ? "draft" : pr.State).toLowerCase()}
              class="preview-badge"
            />
          </div>
          <div class="preview-subtitle">
            {pr.repo_owner}/{pr.repo_name} #{pr.Number}
          </div>
          {#if pr.UpdatedAt}
            <div class="preview-meta">Updated {timeAgo(pr.UpdatedAt)}</div>
          {/if}
          {#if pr.Body}
            <div class="preview-body">{bodyExcerpt(pr.Body)}</div>
          {/if}
        {:else}
          {@const issue = highlighted.item}
          <div class="preview-header">
            <div class="preview-title">{issue.Title}</div>
            <ItemStateChip
              state={issue.State.toLowerCase()}
              class="preview-badge"
            />
          </div>
          <div class="preview-subtitle">
            {issue.repo_owner}/{issue.repo_name} #{issue.Number}
          </div>
          {#if issue.UpdatedAt}
            <div class="preview-meta">Updated {timeAgo(issue.UpdatedAt)}</div>
          {/if}
          {#if issue.Body}
            <div class="preview-body">{bodyExcerpt(issue.Body)}</div>
          {/if}
        {/if}
      </div>
    </div>
    <div class="palette-footer">
      <span>up/down navigate</span>
      <span>enter run</span>
      <span>esc close</span>
    </div>
  </div>
{/if}

<style>
  .palette-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
    z-index: 100;
  }

  .palette {
    position: fixed;
    top: 80px;
    left: 50%;
    transform: translateX(-50%);
    width: 920px;
    max-width: calc(100vw - 32px);
    height: 480px;
    display: grid;
    grid-template-rows: auto 1fr auto;
    background: var(--bg-surface);
    border: 1px solid var(--border-default);
    border-radius: 10px;
    box-shadow: var(--shadow-lg);
    z-index: 101;
  }

  .palette-input {
    padding: 12px 16px;
    border: none;
    border-bottom: 1px solid var(--border-muted);
    background: transparent;
    color: var(--text-primary);
    font-size: 14px;
    outline: none;
  }

  .palette-body {
    display: grid;
    grid-template-columns: 360px 1fr;
    overflow: hidden;
  }

  .palette-list {
    border-right: 1px solid var(--border-muted);
    overflow-y: auto;
  }

  .palette-group {
    padding: 4px 0;
  }

  .palette-group-header {
    padding: 6px 16px 4px;
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-secondary);
  }

  .palette-row {
    width: 100%;
    display: flex;
    align-items: baseline;
    gap: 10px;
    padding: 6px 16px;
    background: transparent;
    border: none;
    color: var(--text-primary);
    font-size: 13px;
    text-align: left;
    cursor: pointer;
  }

  .palette-row:hover,
  .palette-row:focus-visible {
    background: var(--bg-surface-hover);
    outline: none;
  }

  .palette-row-highlight {
    background: var(--bg-surface-hover);
    box-shadow: inset 2px 0 0 0 var(--accent-blue, var(--text-primary));
  }

  .palette-row-label {
    flex: 1 1 auto;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .palette-row-tag {
    color: var(--text-secondary);
    font-size: 11px;
    flex: 0 0 auto;
  }

  .palette-row-disabled {
    color: var(--text-muted);
    cursor: default;
  }

  .palette-row-disabled:hover,
  .palette-row-disabled:focus-visible {
    background: transparent;
  }

  .palette-row-disabled code {
    font-family: var(--font-mono, monospace);
    font-size: 11px;
    padding: 1px 4px;
    border-radius: 3px;
    background: var(--bg-surface-hover);
    margin: 0 2px;
  }

  .palette-preview {
    padding: 16px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .preview-empty {
    color: var(--text-muted);
    font-size: 12px;
    font-style: italic;
  }

  .preview-header {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .preview-title {
    color: var(--text-primary);
    font-size: 14px;
    font-weight: 600;
    flex: 1 1 auto;
  }

  .preview-subtitle {
    color: var(--text-secondary);
    font-size: 12px;
  }

  .preview-meta {
    color: var(--text-muted);
    font-size: 11px;
  }

  .preview-body {
    color: var(--text-primary);
    font-size: 12px;
    line-height: 1.5;
    white-space: pre-wrap;
    margin-top: 4px;
  }

  .palette-footer {
    padding: 6px 12px;
    border-top: 1px solid var(--border-muted);
    font-size: 11px;
    color: var(--text-secondary);
    display: flex;
    gap: 16px;
  }
</style>
