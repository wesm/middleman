import { expect, test, type Page, type Route } from "@playwright/test";

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

function requestBodyPlatformHost(route: Route): string {
  const body = route.request().postDataJSON() as { platform_host?: string } | null;
  return body?.platform_host ?? "";
}

async function setupHostedPR(page: Page): Promise<Record<string, string[]>> {
  await mockApi(page);

  const seen: Record<string, string[]> = {
    detail: [],
    asyncSync: [],
    state: [],
    githubState: [],
    content: [],
    comments: [],
    files: [],
    diff: [],
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
      seen.state.push(requestBodyPlatformHost(route));
      await route.fulfill({ status: 200 });
    },
  );

  await page.route(
    /\/api\/v1\/repos\/acme\/widgets\/pulls\/42\/github-state(?:[?]|$)/,
    async (route) => {
      seen.githubState.push(requestBodyPlatformHost(route));
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ state: "closed" }),
      });
    },
  );

  await page.route(
    /\/api\/v1\/repos\/acme\/widgets\/pulls\/42\/comments(?:[?]|$)/,
    async (route) => {
      seen.comments.push(requestBodyPlatformHost(route));
      await route.fulfill({
        status: 201,
        contentType: "application/json",
        body: JSON.stringify({ ID: 99, Kind: "comment", Body: "queued" }),
      });
    },
  );

  await page.route(
    /\/api\/v1\/repos\/acme\/widgets\/pulls\/42\/files(?:[?]|$)/,
    async (route) => {
      const url = new URL(route.request().url());
      seen.files.push(url.searchParams.get("platform_host") ?? "");
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          stale: false,
          files: [
            {
              path: "src/App.svelte",
              old_path: "src/App.svelte",
              status: "modified",
              is_binary: false,
              is_whitespace_only: false,
              additions: 10,
              deletions: 2,
              hunks: [],
            },
          ],
        }),
      });
    },
  );

  await page.route(
    /\/api\/v1\/repos\/acme\/widgets\/pulls\/42\/diff(?:[?]|$)/,
    async (route) => {
      const url = new URL(route.request().url());
      seen.diff.push(url.searchParams.get("platform_host") ?? "");
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          stale: false,
          whitespace_only_count: 0,
          files: [
            {
              path: "src/App.svelte",
              old_path: "src/App.svelte",
              status: "modified",
              is_binary: false,
              is_whitespace_only: false,
              additions: 10,
              deletions: 2,
              hunks: [],
            },
          ],
        }),
      });
    },
  );

  await page.route(
    /\/api\/v1\/repos\/acme\/widgets\/pulls\/42(?:[?]|$)/,
    async (route) => {
      if (route.request().method() !== "PATCH") return route.fallback();
      seen.content.push(requestBodyPlatformHost(route));
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(hostedPRDetail()),
      });
    },
  );

  await page.route(
    /\/api\/v1\/repos\/acme\/widgets\/pulls\/42\/ready-for-review(?:[?]|$)/,
    async (route) => {
      seen.ready.push(requestBodyPlatformHost(route));
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
      seen.approve.push(requestBodyPlatformHost(route));
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
      seen.workflows.push(requestBodyPlatformHost(route));
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
      seen.merge.push(requestBodyPlatformHost(route));
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

  await page.locator(".btn--close").click();
  await expect.poll(() => seen.githubState).toContain(platformHost);

  await page.locator(".body-section .edit-body-btn").click();
  await page.locator(".body-edit-textarea").fill("Updated body");
  await page.locator(".body-edit .title-edit-save").click();
  await expect.poll(() => seen.content).toContain(platformHost);

  await page.locator(".comment-editor-input").fill("Queued comment");
  await page.getByRole("button", { name: "Comment" }).click();
  await expect.poll(() => seen.comments).toContain(platformHost);

  await page.getByRole("button", { name: "Files changed" }).click();
  await expect.poll(() => seen.files).toContain(platformHost);
  await expect.poll(() => seen.diff).toContain(platformHost);

  await page.getByRole("button", { name: "Conversation" }).click();

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
