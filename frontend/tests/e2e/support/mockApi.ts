import type { Page, Route } from "@playwright/test";

const pulls = [
  {
    ID: 1,
    RepoID: 1,
    GitHubID: 101,
    Number: 42,
    URL: "https://github.com/acme/widgets/pull/42",
    Title: "Add browser regression coverage",
    Author: "marius",
    State: "open",
    IsDraft: false,
    Body: "",
    HeadBranch: "feature/playwright",
    BaseBranch: "main",
    Additions: 120,
    Deletions: 12,
    CommentCount: 3,
    ReviewDecision: "APPROVED",
    CIStatus: "success",
    CIChecksJSON: "[]",
    CreatedAt: "2026-03-29T14:00:00Z",
    UpdatedAt: "2026-03-30T14:00:00Z",
    LastActivityAt: "2026-03-30T14:00:00Z",
    MergedAt: null,
    ClosedAt: null,
    KanbanStatus: "reviewing",
    Starred: false,
    repo_owner: "acme",
    repo_name: "widgets",
  },
];

const issues = [
  {
    ID: 2,
    RepoID: 1,
    GitHubID: 202,
    Number: 7,
    URL: "https://github.com/acme/widgets/issues/7",
    Title: "Theme toggle does not stick",
    Author: "marius",
    State: "open",
    Body: "",
    CommentCount: 1,
    LabelsJSON: "[]",
    CreatedAt: "2026-03-28T14:00:00Z",
    UpdatedAt: "2026-03-30T14:00:00Z",
    LastActivityAt: "2026-03-30T14:00:00Z",
    ClosedAt: null,
    Starred: false,
    repo_owner: "acme",
    repo_name: "widgets",
  },
];

const repos = [
  {
    ID: 1,
    Owner: "acme",
    Name: "widgets",
    AllowSquashMerge: true,
    AllowMergeCommit: true,
    AllowRebaseMerge: true,
    LastSyncStartedAt: "2026-03-30T14:00:00Z",
    LastSyncCompletedAt: "2026-03-30T14:00:30Z",
    LastSyncError: "",
    CreatedAt: "2026-03-01T00:00:00Z",
  },
];

const syncStatus = {
  running: false,
  last_run_at: "2026-03-30T14:00:30Z",
  last_error: "",
};

async function fulfillJson(route: Route, body: unknown, status = 200): Promise<void> {
  await route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

export async function mockApi(page: Page): Promise<void> {
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const { pathname } = url;
    const method = route.request().method();

    if (method === "GET" && pathname === "/api/v1/pulls") {
      await fulfillJson(route, pulls);
      return;
    }

    if (method === "GET" && pathname === "/api/v1/issues") {
      await fulfillJson(route, issues);
      return;
    }

    if (method === "GET" && pathname === "/api/v1/repos") {
      await fulfillJson(route, repos);
      return;
    }

    if (method === "GET" && pathname === "/api/v1/sync/status") {
      await fulfillJson(route, syncStatus);
      return;
    }

    if (method === "POST" && pathname === "/api/v1/sync") {
      await fulfillJson(route, undefined, 202);
      return;
    }

    await fulfillJson(route, { error: `Unhandled ${method} ${pathname}` }, 404);
  });
}
