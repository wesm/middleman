import type { ConfigRepo } from "../api/types.js";

export interface MobileActivityRepoOption {
  value: string;
  label: string;
}

function concreteRepoSelectorValue(repo: ConfigRepo): string | null {
  const repoPath = repo.repo_path?.trim();
  const platformHost = repo.platform_host?.trim();
  if (!repoPath || !platformHost || repo.is_glob) return null;
  return `${platformHost}/${repoPath}`;
}

export function buildMobileActivityRepoOptions(
  repos: ConfigRepo[],
): MobileActivityRepoOption[] {
  const seen = new Set<string>();
  const options: MobileActivityRepoOption[] = [];
  for (const repo of repos) {
    const value = concreteRepoSelectorValue(repo);
    if (!value || seen.has(value)) continue;
    seen.add(value);
    options.push({ value, label: value });
  }
  return options;
}
