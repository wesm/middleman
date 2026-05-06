<script lang="ts">
  import { onDestroy } from "svelte";
  import type { SplitResizeEvent } from "./split-resize.js";

  interface Props {
    ariaLabel: string;
    class?: string;
    keyboardStep?: number;
    onResizeStart?: (event: KeyboardEvent | MouseEvent) => void;
    onResize?: (event: SplitResizeEvent) => void;
    onResizeEnd?: (event: SplitResizeEvent) => void;
  }

  let {
    ariaLabel,
    class: className = "",
    keyboardStep = 24,
    onResizeStart,
    onResize,
    onResizeEnd,
  }: Props = $props();

  let cleanup: (() => void) | null = null;

  function stopResize(): void {
    cleanup?.();
    cleanup = null;
  }

  function startResize(event: MouseEvent): void {
    event.preventDefault();
    stopResize();
    const startX = event.clientX;
    let lastEvent: SplitResizeEvent = {
      deltaX: 0,
      startX,
      currentX: startX,
      event,
    };

    onResizeStart?.(event);

    function onMove(moveEvent: MouseEvent): void {
      lastEvent = {
        deltaX: moveEvent.clientX - startX,
        startX,
        currentX: moveEvent.clientX,
        event: moveEvent,
      };
      onResize?.(lastEvent);
    }

    function onUp(upEvent: MouseEvent): void {
      lastEvent = {
        deltaX: upEvent.clientX - startX,
        startX,
        currentX: upEvent.clientX,
        event: upEvent,
      };
      onResizeEnd?.(lastEvent);
      stopResize();
    }

    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    cleanup = () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
  }

  function handleKeydown(event: KeyboardEvent): void {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    event.preventDefault();
    const deltaX = event.key === "ArrowLeft" ? -keyboardStep : keyboardStep;
    const resizeEvent: SplitResizeEvent = {
      deltaX,
      startX: 0,
      currentX: deltaX,
      event,
    };
    onResizeStart?.(event);
    onResize?.(resizeEvent);
    onResizeEnd?.(resizeEvent);
  }

  onDestroy(() => {
    stopResize();
  });
</script>

<button
  class={["split-resize-handle", className]}
  type="button"
  aria-label={ariaLabel}
  onkeydown={handleKeydown}
  onmousedown={startResize}
></button>

<style>
  .split-resize-handle {
    width: 4px;
    cursor: col-resize;
    background: var(--border-muted);
    appearance: none;
    border: 0;
    padding: 0;
    flex-shrink: 0;
  }

  .split-resize-handle:hover,
  .split-resize-handle:focus-visible {
    background: var(--accent-blue);
    outline: none;
  }
</style>
