const STORAGE_KEY = "middleman:groupByRepo";

function readFromStorage(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) !== "false";
  } catch {
    return true;
  }
}

let groupByRepo = $state(readFromStorage());

export function getGroupByRepo(): boolean {
  return groupByRepo;
}

export function setGroupByRepo(value: boolean): void {
  groupByRepo = value;
  try {
    localStorage.setItem(STORAGE_KEY, String(value));
  } catch {
    // localStorage unavailable (e.g., private browsing quota).
  }
}
