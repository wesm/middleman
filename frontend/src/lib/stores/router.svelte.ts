type View = "list" | "board";
type Tab = "pulls" | "issues";

let currentView = $state<View>("list");
let currentTab = $state<Tab>("pulls");

export function getView(): View {
  return currentView;
}

export function setView(v: View): void {
  currentView = v;
}

export function getTab(): Tab {
  return currentTab;
}

export function setTab(t: Tab): void {
  currentTab = t;
}
