import { expect, test, type Page } from "@playwright/test";

import { mockApi } from "./support/mockApi";

const platformHost = "ghe.example.com";

const hostedPR = {
  ID: 42,
  RepoID: 2,
  GitHubID: 4242,
  Number: 42,
  URL: "https://ghe.example.com/acme/widgets/pull/42",
  Title: "Host qualified PR",
  Author: "marius",
  State: "open",
  IsDraft: true,
  Body: "Tracks #7 before merge.",
  HeadBranch: "mirror/host-qualified",
  BaseBranch: "main",
  Additions: 12,
  Deletions: 3,
  CommentCount: 1,
  ReviewDecision: "APPROVED",
  CIStatus: "success",
  CIChecksJSON: "[]",
  CreatedAt: "2026-04-01T12:00:00Z",
  UpdatedAt: "2026-04-01T12:00:00Z",
  LastActivityAt: "2026-04-01T12:00:00Z",
  MergedAt: null,
  ClosedAt: null,
  KanbanStatus: "awaiting_merge",
  Starred: false,
  repo_owner: "acme",
  repo_name: "widgets",
  platform_host: platformHost,
  worktree_links: [],
  MergeableState: "clean",
};

function hostedPRDetail() {
  return {
    merge_request: hostedPR,
    events: [],
    repo_owner: hostedPR.repo_owner,
    repo_name: hostedPR.repo_name,
    platform_host: platformHost,
    platform_head_sha: "head-sha",
    platform_base_sha: "base-sha",
    diff_head_sha: "head-sha",
    merge_base_sha: "base-sha",
    worktree_links: [],
    workflow_approval: {
      checked: true,
      required: true,
      count: 2,
    },
    warnings: [],
    detail_loaded: true,
    detail_fetched_at: "2026-04-01T12:00:00Z",
  };
}

async function setupHostedPR(page: Page): Promise<Record<string, string[]>> {
  await mockApi(page);

  const seen: Record<string, string[]> = {
    detail: [],
    asyncSync: [],
    state: [],
    ready: [],
    approve: [],
    workflows: [],
    merge: [],
    resolve: [],
  };

  await page.route("**/api/v1/settings", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        repos: [
          {
            owner: "acme",
            name: "widgets",
            is_glob: false,
            matched_repo_count: 2,
          },
        ],
        activity: { hidden_authors: [] },
        terminal: { font_family: "" },
      }),
    });
  });

  await page.route(
    /\/api\/v1\/repos\/acme\/widgets\/pulls\/42(?:[?]|$)/,
    async (route) => {
      const url = new URL(route.request().url());
      seen.detail.push(url.searchParams.get("platform_host") ?? "");
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(hostedPRDetail()),
      });
    },
  );

  await page.route(
    /\/api\/v1\/repos\/acme\/widgets\/pulls\/42\/sync\/async(?:[?]|$)/,
    async (route) => {
      const url = new URL(route.request().url());
      seen.asyncSync.push(url.searchParams.get("platform_host") ?? "");
      await route.fulfill({ status: 202 });
    },
  );

  await page.route(
    /\/api\/v1\/repos\/acme\/widgets\/pulls\/42\/state(?:[?]|$)/,
    async (route) => {
      const url = new URL(route.request().url());
      seen.state.push(url.searchParams.get("platform_host") ?? "");
      await route.fulfill({ status: 200 });
    },
  );

  await page.route(
    /\/api\/v1\/repos\/acme\/widgets\/pulls\/42\/ready-for-review(?:[?]|$)/,
    async (route) => {
      const url = new URL(route.request().url());
      seen.ready.push(url.searchParams.get("platform_host") ?? "");
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ status: "ready_for_review" }),
      });
    },
  );

  await page.route(
    /\/api\/v1\/repos\/acme\/widgets\/pulls\/42\/approve(?:[?]|$)/,
    async (route) => {
      const url = new URL(route.request().url());
      seen.approve.push(url.searchParams.get("platform_host") ?? "");
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ status: "approved" }),
      });
    },
  );

  await page.route(
    /\/api\/v1\/repos\/acme\/widgets\/pulls\/42\/approve-workflows(?:[?]|$)/,
    async (route) => {
      const url = new URL(route.request().url());
      seen.workflows.push(url.searchParams.get("platform_host") ?? "");
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ status: "approved_workflows" }),
      });
    },
  );

  await page.route(
    /\/api\/v1\/repos\/acme\/widgets\/pulls\/42\/merge(?:[?]|$)/,
    async (route) => {
      const url = new URL(route.request().url());
      seen.merge.push(url.searchParams.get("platform_host") ?? "");
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ merged: true, sha: "merge-sha" }),
      });
    },
  );

  await page.route(
    /\/api\/v1\/repos\/acme\/widgets\/items\/7\/resolve(?:[?]|$)/,
    async (route) => {
      const url = new URL(route.request().url());
      seen.resolve.push(url.searchParams.get("platform_host") ?? "");
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          item_type: "pr",
          number: 7,
          repo_tracked: true,
        }),
      });
    },
  );

  return seen;
}

test("host-qualified PR detail actions preserve platform_host", async ({ page }) => {
  const seen = await setupHostedPR(page);

  await page.goto("/pulls/acme/widgets/42?platform_host=ghe.example.com");
  await expect(page.getByRole("heading", { name: "Host qualified PR" }))
    .toBeVisible();
  await expect.poll(() => seen.detail).toContain(platformHost);
  await expect.poll(() => seen.asyncSync).toContain(platformHost);

  await page.getByRole("combobox", { name: /change workflow status/i }).click();
  await page.getByRole("option", { name: "Reviewing" }).click();
  await expect.poll(() => seen.state).toContain(platformHost);

  await page.locator(".btn--ready").click();
  await expect.poll(() => seen.ready).toContain(platformHost);

  await page.locator(".btn--approve").click();
  await page.getByRole("button", { name: /^Approve$/ }).click();
  await expect.poll(() => seen.approve).toContain(platformHost);

  await page.locator(".btn--workflow-approval").click();
  await expect.poll(() => seen.workflows).toContain(platformHost);

  await page.locator(".btn--merge").click();
  await page.getByRole("button", { name: "Squash and merge" }).click();
  await expect.poll(() => seen.merge).toContain(platformHost);

  await page.locator(".markdown-body .item-ref", { hasText: "#7" }).click();
  await expect.poll(() => seen.resolve).toContain(platformHost);
  await expect(page).toHaveURL(/\/pulls\/acme\/widgets\/7\?platform_host=ghe\.example\.com$/);
});
