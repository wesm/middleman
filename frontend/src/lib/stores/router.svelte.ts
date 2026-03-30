type View = "list" | "board";

let currentView = $state<View>("list");

export function getView(): View {
  return currentView;
}

export function setView(v: View): void {
  currentView = v;
}
