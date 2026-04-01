const STORAGE_KEY = "middleman-filter-repo";

function loadPersistedRepo(): string | undefined {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    return v ?? undefined;
  } catch {
    return undefined;
  }
}

let filterRepo = $state<string | undefined>(loadPersistedRepo());

export function getGlobalRepo(): string | undefined {
  return filterRepo;
}

export function setGlobalRepo(repo: string | undefined): void {
  filterRepo = repo;
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
