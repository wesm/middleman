const STORAGE_KEY = "middleman:groupByRepo";

function readFromStorage(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) !== "false";
  } catch {
    return true;
  }
}

export function createGroupingStore() {
  let groupByRepo = $state(readFromStorage());

  function getGroupByRepo(): boolean {
    return groupByRepo;
  }

  function setGroupByRepo(value: boolean): void {
    groupByRepo = value;
    try {
      localStorage.setItem(STORAGE_KEY, String(value));
    } catch {
      // localStorage unavailable (e.g., private browsing quota).
    }
  }

  return {
    getGroupByRepo,
    setGroupByRepo,
  };
}

export type GroupingStore = ReturnType<typeof createGroupingStore>;
