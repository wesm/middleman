import type { Action, CheatsheetEntry } from "./types.js";

let actionsByOwner = $state<Map<string, Action[]>>(new Map());
let cheatsheetByOwner = $state<Map<string, CheatsheetEntry[]>>(new Map());

export function registerScopedActions(
  ownerId: string,
  actions: Action[],
): () => void {
  actionsByOwner = new Map(actionsByOwner.set(ownerId, [...actions]));
  return () => {
    if (actionsByOwner.has(ownerId)) {
      const next = new Map(actionsByOwner);
      next.delete(ownerId);
      actionsByOwner = next;
    }
  };
}

export function registerCheatsheetEntries(
  ownerId: string,
  entries: CheatsheetEntry[],
): () => void {
  cheatsheetByOwner = new Map(cheatsheetByOwner.set(ownerId, [...entries]));
  return () => {
    if (cheatsheetByOwner.has(ownerId)) {
      const next = new Map(cheatsheetByOwner);
      next.delete(ownerId);
      cheatsheetByOwner = next;
    }
  };
}

export function getActionsByOwner(ownerId: string): Action[] {
  return actionsByOwner.get(ownerId) ?? [];
}

export function getAllActions(): Action[] {
  const out: Action[] = [];
  for (const list of actionsByOwner.values()) out.push(...list);
  return out;
}

export function getAllCheatsheetEntries(): CheatsheetEntry[] {
  const out: CheatsheetEntry[] = [];
  for (const list of cheatsheetByOwner.values()) out.push(...list);
  return out;
}

export function resetRegistry(): void {
  actionsByOwner = new Map();
  cheatsheetByOwner = new Map();
}
