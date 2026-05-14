let open = $state(false);
let lastFocusedElement: HTMLElement | null = null;

export function openPalette(): void {
  if (typeof document !== "undefined") {
    lastFocusedElement = document.activeElement as HTMLElement | null;
  }
  open = true;
}

export function closePalette(): void {
  open = false;
  if (lastFocusedElement && typeof lastFocusedElement.focus === "function") {
    lastFocusedElement.focus();
    lastFocusedElement = null;
  }
}

export function togglePalette(): void {
  if (open) {
    closePalette();
  } else {
    openPalette();
  }
}

export function isPaletteOpen(): boolean {
  return open;
}

export function resetPaletteState(): void {
  open = false;
  lastFocusedElement = null;
}
