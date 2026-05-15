import { describe, expect, it, vi } from "vitest";

import { createDetailStore } from "@middleman/ui/stores/detail";
import { createIssuesStore } from "@middleman/ui/stores/issues";
import type { MiddlemanClient } from "@middleman/ui";

describe("provider-aware detail API routes", () => {
  it("loads PR detail through the provider item endpoint", async () => {
    const client = {
      GET: vi.fn(async () => ({
        data: {
          repo_owner: "Group/SubGroup",
          repo_name: "Project",
          merge_request: { Number: 12 },
          events: [],
        },
      })),
      POST: vi.fn(),
      PUT: vi.fn(),
      DELETE: vi.fn(),
    } as unknown as MiddlemanClient;
    const store = createDetailStore({ client });

    await store.loadDetail("Group/SubGroup", "Project", 12, {
      sync: false,
      provider: "gitlab",
      platformHost: "gitlab.example.com:8443",
      repoPath: "Group/SubGroup/Project",
    } as never);

    expect(client.GET).toHaveBeenCalledWith("/host/{platform_host}/pulls/{provider}/{owner}/{name}/{number}", {
      params: {
        path: {
          provider: "gitlab",
          platform_host: "gitlab.example.com:8443",
          owner: "Group/SubGroup",
          name: "Project",
          number: 12,
        },
      },
    });
  });

  it("refreshes pending PR CI through the provider sync endpoint", async () => {
    const detail = {
      repo_owner: "Group/SubGroup",
      repo_name: "Project",
      repo: {
        provider: "gitlab",
        platform_host: "gitlab.example.com:8443",
        owner: "Group/SubGroup",
        name: "Project",
        repo_path: "Group/SubGroup/Project",
      },
      merge_request: { Number: 12 },
      events: [],
    };
    const client = {
      GET: vi.fn(async () => ({
        data: detail,
      })),
      POST: vi.fn(async () => ({
        data: detail,
      })),
      PUT: vi.fn(),
      DELETE: vi.fn(),
    } as unknown as MiddlemanClient;
    const store = createDetailStore({ client });

    await store.loadDetail("Group/SubGroup", "Project", 12, {
      sync: false,
      provider: "gitlab",
      platformHost: "gitlab.example.com:8443",
      repoPath: "Group/SubGroup/Project",
    } as never);

    await store.refreshPendingCI("Group/SubGroup", "Project", 12, {
      provider: "gitlab",
      platformHost: "gitlab.example.com:8443",
      repoPath: "Group/SubGroup/Project",
    });

    expect(client.POST).toHaveBeenCalledWith("/host/{platform_host}/pulls/{provider}/{owner}/{name}/{number}/sync", {
      params: {
        path: {
          provider: "gitlab",
          platform_host: "gitlab.example.com:8443",
          owner: "Group/SubGroup",
          name: "Project",
          number: 12,
        },
      },
    });
  });

  it("loads issue detail through the provider item endpoint", async () => {
    const client = {
      GET: vi.fn(async () => ({
        data: {
          repo_owner: "Group/SubGroup",
          repo_name: "Project",
          issue: { Number: 7 },
          events: [],
        },
      })),
      POST: vi.fn(),
      PUT: vi.fn(),
      DELETE: vi.fn(),
    } as unknown as MiddlemanClient;
    const store = createIssuesStore({ client });

    await store.loadIssueDetail("Group/SubGroup", "Project", 7, {
      sync: false,
      provider: "gitlab",
      platformHost: "gitlab.example.com:8443",
      repoPath: "Group/SubGroup/Project",
    } as never);

    expect(client.GET).toHaveBeenCalledWith("/host/{platform_host}/issues/{provider}/{owner}/{name}/{number}", {
      params: {
        path: {
          provider: "gitlab",
          platform_host: "gitlab.example.com:8443",
          owner: "Group/SubGroup",
          name: "Project",
          number: 7,
        },
      },
    });
  });
});
