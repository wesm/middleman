export type GroupingMode = "flat" | "byRepo" | "byWorkflow";

const STORAGE_KEY = "middleman:groupingMode";

// Legacy key for backward-compat reads.
const LEGACY_KEY = "middleman:groupByRepo";

function readFromStorage(): GroupingMode {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (
      raw === "flat" ||
      raw === "byRepo" ||
      raw === "byWorkflow"
    ) {
      return raw;
    }
    // Migrate from legacy boolean key.
    const legacy = localStorage.getItem(LEGACY_KEY);
    if (legacy === "false") return "flat";
    return "byRepo";
  } catch {
    return "byRepo";
  }
}

export function createGroupingStore() {
  let mode = $state<GroupingMode>(readFromStorage());

  function getGroupingMode(): GroupingMode {
    return mode;
  }

  function setGroupingMode(value: GroupingMode): void {
    mode = value;
    try {
      localStorage.setItem(STORAGE_KEY, value);
    } catch {
      // localStorage unavailable.
    }
  }

  /** Backward-compat: true when mode is "byRepo". */
  function getGroupByRepo(): boolean {
    return mode === "byRepo";
  }

  /** Backward-compat: sets mode to "byRepo" or "flat". */
  function setGroupByRepo(value: boolean): void {
    setGroupingMode(value ? "byRepo" : "flat");
  }

  return {
    getGroupingMode,
    setGroupingMode,
    getGroupByRepo,
    setGroupByRepo,
  };
}

export type GroupingStore = ReturnType<
  typeof createGroupingStore
>;
