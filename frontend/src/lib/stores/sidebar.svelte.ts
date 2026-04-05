import { getUIConfig } from "./embed-config.svelte.js";

const STORAGE_KEY = "middleman-sidebar";

let collapsed = $state(false);
// When the container enters narrow mode, we force-collapse the sidebar.
// narrowCollapsed forces closed; narrowOpened forces open (user override).
// Both are cleared when leaving narrow mode.
let narrowCollapsed = $state(false);
let narrowOpened = $state(false);

function loadPersisted(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) === "collapsed";
  } catch {
    return false;
  }
}

function persist(value: boolean): void {
  try {
    if (value) {
      localStorage.setItem(STORAGE_KEY, "collapsed");
    } else {
      localStorage.removeItem(STORAGE_KEY);
    }
  } catch {
    // Storage blocked
  }
}

export function initSidebar(): void {
  const ui = getUIConfig();
  if (ui.sidebarCollapsed !== undefined) {
    collapsed = ui.sidebarCollapsed;
  } else {
    collapsed = loadPersisted();
  }
}

export function isSidebarCollapsed(): boolean {
  if (narrowOpened) return false;
  if (narrowCollapsed) return true;
  return collapsed;
}

export function isSidebarToggleEnabled(): boolean {
  return getUIConfig().sidebarCollapsed === undefined;
}

export function toggleSidebar(): void {
  if (!isSidebarToggleEnabled()) return;
  if (narrowCollapsed || narrowOpened) {
    // Toggle the transient narrow state without touching the
    // persisted preference. Both flags reset on widen.
    const wasOpen = narrowOpened;
    narrowOpened = !wasOpen;
    narrowCollapsed = wasOpen;
    return;
  }
  collapsed = !collapsed;
  persist(collapsed);
}

export function setSidebarCollapsed(value: boolean): void {
  collapsed = value;
}

export function setNarrowOverride(narrow: boolean): void {
  if (narrow) {
    // Only reset on transition into narrow, not repeated calls.
    if (!narrowCollapsed && !narrowOpened) {
      narrowCollapsed = true;
    }
  } else {
    narrowCollapsed = false;
    narrowOpened = false;
  }
}
