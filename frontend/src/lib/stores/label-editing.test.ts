import { describe, expect, it, vi } from "vitest";

import { createDetailStore } from "@middleman/ui/stores/detail";
import { createIssuesStore } from "@middleman/ui/stores/issues";
import type { MiddlemanClient } from "@middleman/ui";
import type { Label } from "@middleman/ui/api/types";

const routeRef = { provider: "github", platformHost: "github.com", repoPath: "octo/repo" };

function label(name: string): Label {
  return { name, color: name === "bug" ? "d73a4a" : "fbca04", is_default: false };
}

describe("label editing stores", () => {
  it("updates visible pull labels from the label mutation response", async () => {
    const client = {
      GET: vi.fn(async () => ({
        data: {
          repo_owner: "octo",
          repo_name: "repo",
          repo: { provider: "github", platform_host: "github.com", owner: "octo", name: "repo", repo_path: "octo/repo" },
          merge_request: { Number: 1, labels: [label("bug")] },
          events: [],
        },
      })),
      POST: vi.fn(async () => ({ data: undefined })),
      PUT: vi.fn(async () => ({ data: { labels: [label("triage")] } })),
      DELETE: vi.fn(),
    } as unknown as MiddlemanClient;
    const store = createDetailStore({ client });

    await store.loadDetail("octo", "repo", 1, routeRef);
    await store.setPullLabels("octo", "repo", 1, ["triage"]);

    expect(client.PUT).toHaveBeenCalledWith(
      "/pulls/{provider}/{owner}/{name}/{number}/labels",
      expect.objectContaining({ body: { labels: ["triage"] } }),
    );
    expect(store.getDetail()?.merge_request.labels?.map((item) => item.name)).toEqual(["triage"]);
  });

  it("updates visible issue labels from the label mutation response", async () => {
    const client = {
      GET: vi.fn(async () => ({
        data: {
          repo_owner: "octo",
          repo_name: "repo",
          repo: { provider: "github", platform_host: "github.com", owner: "octo", name: "repo", repo_path: "octo/repo" },
          issue: { Number: 2, labels: [label("bug")], UpdatedAt: "2026-05-15T12:00:00Z" },
          events: [],
        },
      })),
      POST: vi.fn(async () => ({ data: undefined })),
      PUT: vi.fn(async () => ({ data: { labels: [label("triage")] } })),
      DELETE: vi.fn(),
    } as unknown as MiddlemanClient;
    const store = createIssuesStore({ client });

    await store.loadIssueDetail("octo", "repo", 2, routeRef);
    await store.setIssueLabels("octo", "repo", 2, ["triage"]);

    expect(client.PUT).toHaveBeenCalledWith(
      "/issues/{provider}/{owner}/{name}/{number}/labels",
      expect.objectContaining({ body: { labels: ["triage"] } }),
    );
    expect(store.getIssueDetail()?.issue.labels?.map((item) => item.name)).toEqual(["triage"]);
  });
});
