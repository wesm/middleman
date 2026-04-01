import type { ConfigRepo } from "../api/types.js";

let repos = $state<ConfigRepo[]>([]);

export function getConfiguredRepos(): ConfigRepo[] {
  return repos;
}

export function setConfiguredRepos(r: ConfigRepo[]): void {
  repos = r ?? [];
}

export function hasConfiguredRepos(): boolean {
  return repos.length > 0;
}
