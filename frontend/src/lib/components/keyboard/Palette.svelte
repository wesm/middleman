<script lang="ts">
  import { tick, untrack } from "svelte";

  import { pushModalFrame } from "@middleman/ui/stores/keyboard/modal-stack";
  import type { ModalFrameAction } from "@middleman/ui/stores/keyboard/keyspec";
  import { getStores } from "@middleman/ui";
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

  const parsed = $derived(parsePaletteQuery(query));
  const visibleCommands = $derived.by(() => {
    if (!stores) return [];
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

  function pullKey(repoOwner: string, repoName: string, num: number): string {
    return `${repoOwner}/${repoName}#${num}`;
  }

  function selectRow(): void {
    // Task 19 only renders rows; selection (run/navigate) lands in later tasks.
    closePalette();
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
              {#each grouped.commands as command (command.id)}
                <button
                  class="palette-row"
                  type="button"
                  onclick={selectRow}
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
              {#each grouped.pulls as pr (pullKey(pr.repo_owner, pr.repo_name, pr.Number))}
                <button
                  class="palette-row"
                  type="button"
                  onclick={selectRow}
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
              {#each grouped.issues as issue (pullKey(issue.repo_owner, issue.repo_name, issue.Number))}
                <button
                  class="palette-row"
                  type="button"
                  onclick={selectRow}
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
      <div class="palette-preview"></div>
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
