<script lang="ts">
  import { tick, untrack } from "svelte";

  import { pushModalFrame } from "@middleman/ui/stores/keyboard/modal-stack";
  import type { ModalFrameAction } from "@middleman/ui/stores/keyboard/keyspec";
  import {
    closePalette,
    isPaletteOpen,
  } from "../../stores/keyboard/palette-state.svelte.js";

  let dialogEl: HTMLDivElement | undefined = $state();
  let inputEl: HTMLInputElement | undefined = $state();

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
      class="palette-input"
      placeholder="Search loaded PRs, issues, commands..."
    />
    <div class="palette-body">
      <div class="palette-list"></div>
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
