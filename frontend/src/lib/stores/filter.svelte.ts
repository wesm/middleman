const STORAGE_KEY = "middleman-filter-repo";

function loadPersistedRepo(): string | undefined {
  try {
    return localStorage.getItem(STORAGE_KEY) || undefined;
  } catch {
    return undefined;
  }
}

let filterRepo = $state<string | undefined>(loadPersistedRepo());

export function getGlobalRepo(): string | undefined {
  return filterRepo;
}

export function setGlobalRepo(repo: string | undefined): void {
  filterRepo = repo || undefined;
  try {
    if (repo !== undefined) {
      localStorage.setItem(STORAGE_KEY, repo);
    } else {
      localStorage.removeItem(STORAGE_KEY);
    }
  } catch {
    // Storage blocked — filter still works for this session
  }
}
