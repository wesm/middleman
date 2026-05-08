import type { Action, CheatsheetEntry } from "./types.js";

let actionsByOwner = $state<Map<string, Action[]>>(new Map());
let cheatsheetByOwner = $state<Map<string, CheatsheetEntry[]>>(new Map());

export function registerScopedActions(
  ownerId: string,
  actions: Action[],
): () => void {
  const registered = [...actions];
  const next = new Map(actionsByOwner).set(ownerId, registered);
  assertNoConflicts(next);
  actionsByOwner = next;
  return () => {
    if (actionsByOwner.get(ownerId) === registered) {
      const cleanupNext = new Map(actionsByOwner);
      cleanupNext.delete(ownerId);
      actionsByOwner = cleanupNext;
    }
  };
}

function assertNoConflicts(map: Map<string, Action[]>): void {
  const seen = new Map<string, { id: string; owner: string }>();
  for (const [owner, list] of map) {
    for (const action of list) {
      if (action.binding === null) continue;
      const bindings = Array.isArray(action.binding)
        ? action.binding
        : [action.binding];
      for (const b of bindings) {
        const key = `${b.key}|${b.ctrlOrMeta ?? false}|${b.shift ?? false}|${b.alt ?? false}|${action.scope}|${action.priority}`;
        const prior = seen.get(key);
        if (prior) {
          const msg =
            `keyboard registry conflict: action '${prior.id}' (owner '${prior.owner}') ` +
            `and '${action.id}' (owner '${owner}') share binding+scope+priority`;
          if (import.meta.env?.DEV || import.meta.env?.MODE === "test") {
            throw new Error(msg);
          }
          // eslint-disable-next-line no-console
          console.warn(msg);
        } else {
          seen.set(key, { id: action.id, owner });
        }
      }
    }
  }
}

export function registerCheatsheetEntries(
  ownerId: string,
  entries: CheatsheetEntry[],
): () => void {
  const registered = [...entries];
  cheatsheetByOwner = new Map(cheatsheetByOwner).set(ownerId, registered);
  return () => {
    if (cheatsheetByOwner.get(ownerId) === registered) {
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
