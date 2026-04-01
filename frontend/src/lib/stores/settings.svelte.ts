import type { ConfigRepo } from "../api/types.js";

let repos = $state<ConfigRepo[]>([]);
let loaded = $state(false);

export function getConfiguredRepos(): ConfigRepo[] {
  return repos;
}

export function setConfiguredRepos(r: ConfigRepo[]): void {
  repos = r ?? [];
  loaded = true;
}

export function hasConfiguredRepos(): boolean {
  return repos.length > 0;
}

export function isSettingsLoaded(): boolean {
  return loaded;
}
