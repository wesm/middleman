import type { ConfigRepo } from "../api/types.js";

export function createSettingsStore() {
  let repos = $state<ConfigRepo[]>([]);
  let loaded = $state(false);

  function getConfiguredRepos(): ConfigRepo[] {
    return repos;
  }

  function setConfiguredRepos(r: ConfigRepo[]): void {
    repos = r ?? [];
    loaded = true;
  }

  function hasConfiguredRepos(): boolean {
    return repos.length > 0;
  }

  function isSettingsLoaded(): boolean {
    return loaded;
  }

  return {
    getConfiguredRepos,
    setConfiguredRepos,
    hasConfiguredRepos,
    isSettingsLoaded,
  };
}

export type SettingsStore = ReturnType<
  typeof createSettingsStore
>;
