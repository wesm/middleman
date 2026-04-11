import type { MiddlemanClient } from "../types.js";

type StackResponse = {
  id: number;
  name: string;
  repo_owner: string;
  repo_name: string;
  health: string;
  members: StackMemberResponse[];
};

type StackMemberResponse = {
  number: number;
  title: string;
  state: string;
  ci_status: string;
  review_decision: string;
  position: number;
  is_draft: boolean;
  blocked_by: number | null;
};

export interface StacksStoreOptions {
  client: MiddlemanClient;
  getGlobalRepo?: () => string | undefined;
}

export type StacksStore = ReturnType<typeof createStacksStore>;

export function createStacksStore(opts: StacksStoreOptions) {
  const apiClient = opts.client;
  const getGlobalRepo = opts.getGlobalRepo ?? (() => undefined);

  let stacks = $state<StackResponse[]>([]);
  let loading = $state(false);
  let storeError = $state<string | null>(null);
  let requestSeq = 0;

  function getStacks(): StackResponse[] {
    return stacks;
  }

  function isLoading(): boolean {
    return loading;
  }

  function getError(): string | null {
    return storeError;
  }

  function getStacksByRepo(): Map<string, StackResponse[]> {
    const grouped = new Map<string, StackResponse[]>();
    for (const stack of stacks) {
      const key = `${stack.repo_owner}/${stack.repo_name}`;
      const list = grouped.get(key) ?? [];
      list.push(stack);
      grouped.set(key, list);
    }
    return grouped;
  }

  async function loadStacks(): Promise<void> {
    const seq = ++requestSeq;
    loading = true;
    storeError = null;
    try {
      const globalRepo = getGlobalRepo();
      const { data, error } = await apiClient.GET("/stacks", {
        params: {
          query: {
            ...(globalRepo !== undefined && { repo: globalRepo }),
          },
        },
      });
      if (seq !== requestSeq) return;
      if (error) {
        throw new Error(
          error.detail ?? error.title ?? "failed to load stacks",
        );
      }
      stacks = ((data as StackResponse[]) ?? []).map((s) => ({
        ...s,
        members: s.members ?? [],
      }));
    } catch (err) {
      if (seq !== requestSeq) return;
      storeError = err instanceof Error ? err.message : String(err);
    } finally {
      if (seq === requestSeq) loading = false;
    }
  }

  return {
    getStacks,
    getStacksByRepo,
    isLoading,
    getError,
    loadStacks,
  };
}
