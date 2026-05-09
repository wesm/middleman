let open = $state(false);
let lastFocusedElement: HTMLElement | null = null;

export function openCheatsheet(): void {
  if (typeof document !== "undefined") {
    lastFocusedElement = document.activeElement as HTMLElement | null;
  }
  open = true;
}

export function closeCheatsheet(): void {
  open = false;
  if (lastFocusedElement && typeof lastFocusedElement.focus === "function") {
    lastFocusedElement.focus();
    lastFocusedElement = null;
  }
}

export function toggleCheatsheet(): void {
  if (open) {
    closeCheatsheet();
  } else {
    openCheatsheet();
  }
}

export function isCheatsheetOpen(): boolean {
  return open;
}

export function resetCheatsheetState(): void {
  open = false;
  lastFocusedElement = null;
}
