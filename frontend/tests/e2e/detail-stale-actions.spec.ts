import { expect, test, type Page, type Route } from "@playwright/test";
import { mockApi } from "./support/mockApi";

const prA = {
  ID: 11,
  RepoID: 1,
  GitHubID: 1101,
  Number: 100,
  URL: "https://github.com/acme/widgets/pull/100",
  Title: "PR A title",
  Author: "marius",
  State: "open",
  IsDraft: false,
  Body: "Body A",
  HeadBranch: "feature/a",
  BaseBranch: "main",
  Additions: 10,
  Deletions: 1,
  CommentCount: 0,
  ReviewDecision: "",
  CIStatus: "success",
  CIChecksJSON: "[]",
  CreatedAt: "2026-04-01T12:00:00Z",
  UpdatedAt: "2026-04-01T12:00:00Z",
  LastActivityAt: "2026-04-01T12:00:00Z",
  MergedAt: null,
  ClosedAt: null,
  KanbanStatus: "new",
  Starred: false,
  repo_owner: "acme",
  repo_name: "widgets",
  platform_host: "github.com",
  worktree_links: [],
  MergeableState: "",
};

const prB = {
  ...prA,
  ID: 22,
  GitHubID: 1102,
  Number: 200,
  URL: "https://github.com/acme/widgets/pull/200",
  Title: "PR B title",
  Body: "Body B",
  HeadBranch: "feature/b",
};

const prSquashOnly = {
  ...prA,
  ID: 23,
  RepoID: 2,
  GitHubID: 1103,
  Number: 300,
  URL: "https://github.com/acme/squash-only/pull/300",
  Title: "Squash-only PR title",
  Body: "Body C",
  HeadBranch: "feature/c",
  repo_name: "squash-only",
};

const issueX = {
  ID: 31,
  RepoID: 1,
  GitHubID: 1201,
  Number: 300,
  URL: "https://github.com/acme/widgets/issues/300",
  Title: "Issue X title",
  Author: "marius",
  State: "open",
  Body: "Body X",
  CommentCount: 0,
  LabelsJSON: "[]",
  CreatedAt: "2026-04-01T12:00:00Z",
  UpdatedAt: "2026-04-01T12:00:00Z",
  LastActivityAt: "2026-04-01T12:00:00Z",
  ClosedAt: null,
  Starred: false,
  platform_host: "github.com",
  repo_owner: "acme",
  repo_name: "widgets",
};

const issueY = {
  ...issueX,
  ID: 32,
  GitHubID: 1202,
  Number: 400,
  URL: "https://github.com/acme/widgets/issues/400",
  Title: "Issue Y title",
  Body: "Body Y",
};

function detailEnvelopePR(pr: typeof prA): unknown {
  return {
    merge_request: pr,
    repo_owner: pr.repo_owner,
    repo_name: pr.repo_name,
    detail_loaded: true,
    detail_fetched_at: "2026-04-01T12:00:00Z",
    worktree_links: pr.worktree_links,
  };
}

function detailEnvelopeIssue(issue: typeof issueX): unknown {
  return {
    issue,
    events: [],
    platform_host: issue.platform_host,
    repo_owner: issue.repo_owner,
    repo_name: issue.repo_name,
    detail_loaded: true,
    detail_fetched_at: "2026-04-01T12:00:00Z",
  };
}

// Endpoints triggered ONLY by user mutation actions (not the
// detail store's automatic /sync POST or list refreshes).
const USER_MUTATION_PATTERNS = [
  /\/pulls\/\d+\/github-state$/,
  /\/pulls\/\d+\/approve$/,
  /\/pulls\/\d+\/approve-workflows$/,
  /\/pulls\/\d+\/ready-for-review$/,
  /\/pulls\/\d+\/comments$/,
  /\/pulls\/\d+\/merge$/,
  /\/pulls\/\d+\/title$/,
  /\/pulls\/\d+\/body$/,
  /\/pulls\/\d+\/star$/,
  /\/pulls\/\d+\/kanban-status$/,
  /\/issues\/\d+\/github-state$/,
  /\/issues\/\d+\/comments$/,
  /\/issues\/\d+\/star$/,
  /\/api\/v1\/workspaces$/,
];

function recordUserMutations(page: Page): string[] {
  const seen: string[] = [];
  page.on("request", (request) => {
    if (request.method() === "GET") return;
    const path = new URL(request.url()).pathname;
    if (USER_MUTATION_PATTERNS.some((p) => p.test(path))) {
      seen.push(`${request.method()} ${path}`);
    }
  });
  return seen;
}

async function mockSettings(page: Page): Promise<void> {
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
            matched_repo_count: 1,
          },
        ],
        activity: { hidden_authors: [] },
        terminal: { font_family: "" },
      }),
    });
  });
}

async function setupHeldPR(
  page: Page,
  fast: typeof prA,
  slow: typeof prB,
): Promise<{ release: () => void }> {
  await mockApi(page);
  await mockSettings(page);
  let release: () => void = () => {};
  const slowDelay = new Promise<void>((resolve) => {
    release = resolve;
  });

  // Fast PR: instant detail GET.
  await page.route(
    `**/api/v1/repos/${fast.repo_owner}/${fast.repo_name}/pulls/${fast.Number}`,
    async (route: Route) => {
      if (route.request().method() === "GET") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(detailEnvelopePR(fast)),
        });
        return;
      }
      await route.fallback();
    },
  );

  // Slow PR: held until release().
  await page.route(
    `**/api/v1/repos/${slow.repo_owner}/${slow.repo_name}/pulls/${slow.Number}`,
    async (route: Route) => {
      if (route.request().method() === "GET") {
        await slowDelay;
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(detailEnvelopePR(slow)),
        });
        return;
      }
      await route.fallback();
    },
  );

  return { release };
}

async function setupHeldIssue(
  page: Page,
  fast: typeof issueX,
  slow: typeof issueY,
): Promise<{ release: () => void }> {
  await mockApi(page);
  await mockSettings(page);
  let release: () => void = () => {};
  const slowDelay = new Promise<void>((resolve) => {
    release = resolve;
  });

  await page.route(
    `**/api/v1/repos/${fast.repo_owner}/${fast.repo_name}/issues/${fast.Number}`,
    async (route: Route) => {
      if (route.request().method() === "GET") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(detailEnvelopeIssue(fast)),
        });
        return;
      }
      await route.fallback();
    },
  );

  await page.route(
    `**/api/v1/repos/${slow.repo_owner}/${slow.repo_name}/issues/${slow.Number}`,
    async (route: Route) => {
      if (route.request().method() === "GET") {
        await slowDelay;
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(detailEnvelopeIssue(slow)),
        });
        return;
      }
      await route.fallback();
    },
  );

  return { release };
}

test.describe("PR detail merge modal route reset", () => {
  test("merge button is disabled when the PR has merge conflicts", async ({ page }) => {
    await mockApi(page);
    await mockSettings(page);

    const conflictedPR = {
      ...prA,
      MergeableState: "dirty",
    };

    await page.route(
      `**/api/v1/repos/${conflictedPR.repo_owner}/${conflictedPR.repo_name}/pulls/${conflictedPR.Number}`,
      async (route) => {
        if (route.request().method() === "GET") {
          await route.fulfill({
            status: 200,
            contentType: "application/json",
            body: JSON.stringify(detailEnvelopePR(conflictedPR)),
          });
          return;
        }
        await route.fallback();
      },
    );

    await page.goto(
      `/pulls/github/${conflictedPR.repo_owner}/${conflictedPR.repo_name}/${conflictedPR.Number}`,
    );

    await expect(page.locator(".detail-title")).toContainText(
      conflictedPR.Title,
    );
    await expect(page.getByText("This branch has conflicts")).toBeVisible();

    const mergeButton = page.locator(".btn--merge").first();
    await expect(mergeButton).toBeDisabled();
    await mergeButton.click({ force: true });
    await expect(
      page.locator(".modal-title", { hasText: "Merge Pull Request" }),
    ).toHaveCount(0);
  });

  test("merge modal closes when the route changes and does not reopen for the new PR", async ({ page }) => {
    await mockApi(page);
    await mockSettings(page);

    for (const pr of [prA, prB]) {
      await page.route(
        `**/api/v1/repos/${pr.repo_owner}/${pr.repo_name}/pulls/${pr.Number}`,
        async (route) => {
          if (route.request().method() === "GET") {
            await route.fulfill({
              status: 200,
              contentType: "application/json",
              body: JSON.stringify(detailEnvelopePR(pr)),
            });
            return;
          }
          await route.fallback();
        },
      );
    }

    await page.goto(
      `/pulls/github/${prA.repo_owner}/${prA.repo_name}/${prA.Number}`,
    );
    await expect(page.locator(".detail-title")).toContainText(prA.Title);

    // Open the merge modal for PR A.
    await page.locator(".btn--merge").first().click();
    await expect(
      page.locator(".modal-title", { hasText: "Merge Pull Request" }),
    ).toBeVisible();

    // Navigate to PR B. The action-local reset must close the
    // modal as soon as the props change.
    await page.evaluate(([owner, name, number]) => {
      window.history.pushState(
        null,
        "",
        `/pulls/github/${owner}/${name}/${number}`,
      );
      window.dispatchEvent(new PopStateEvent("popstate"));
    }, [prB.repo_owner, prB.repo_name, prB.Number] as const);

    await expect(page.locator(".detail-title")).toContainText(prB.Title);
    await expect(
      page.locator(".modal-title", { hasText: "Merge Pull Request" }),
    ).toHaveCount(0);
  });

  test("merge actions wait for settings that match the selected repo", async ({ page }) => {
    await mockApi(page);
    await mockSettings(page);

    for (const pr of [prA, prSquashOnly]) {
      await page.route(
        `**/api/v1/repos/${pr.repo_owner}/${pr.repo_name}/pulls/${pr.Number}`,
        async (route) => {
          if (route.request().method() === "GET") {
            await route.fulfill({
              status: 200,
              contentType: "application/json",
              body: JSON.stringify(detailEnvelopePR(pr)),
            });
            return;
          }
          await route.fallback();
        },
      );
    }

    let releaseSquashSettings!: () => void;
    const squashSettingsReady = new Promise<void>((resolve) => {
      releaseSquashSettings = resolve;
    });

    await page.route(
      `**/api/v1/repos/${prSquashOnly.repo_owner}/${prSquashOnly.repo_name}`,
      async (route) => {
        if (route.request().method() === "GET") {
          await squashSettingsReady;
          await route.fulfill({
            status: 200,
            contentType: "application/json",
            body: JSON.stringify({
              ID: prSquashOnly.RepoID,
              Owner: prSquashOnly.repo_owner,
              Name: prSquashOnly.repo_name,
              AllowSquashMerge: true,
              AllowMergeCommit: false,
              AllowRebaseMerge: false,
              LastSyncStartedAt: "2026-04-01T12:00:00Z",
              LastSyncCompletedAt: "2026-04-01T12:00:30Z",
              LastSyncError: "",
              CreatedAt: "2026-03-01T00:00:00Z",
            }),
          });
          return;
        }
        await route.fallback();
      },
    );

    await page.goto(
      `/pulls/github/${prA.repo_owner}/${prA.repo_name}/${prA.Number}`,
    );
    await expect(page.locator(".detail-title")).toContainText(prA.Title);
    await expect(page.locator(".btn--merge")).toBeVisible();

    await page.evaluate(([owner, name, number]) => {
      window.history.pushState(
        null,
        "",
        `/pulls/github/${owner}/${name}/${number}`,
      );
      window.dispatchEvent(new PopStateEvent("popstate"));
    }, [prSquashOnly.repo_owner, prSquashOnly.repo_name, prSquashOnly.Number] as const);

    await expect(page.locator(".detail-title")).toContainText(
      prSquashOnly.Title,
    );
    await expect(page.locator(".btn--merge")).toHaveCount(0);

    releaseSquashSettings();

    const mergeButton = page.locator(".btn--merge").first();
    await expect(mergeButton).toBeVisible();
    await mergeButton.click();

    await expect(
      page.locator(".modal-title", { hasText: "Merge Pull Request" }),
    ).toBeVisible();
    await expect(page.locator(".method-option")).toHaveCount(0);
    await expect(
      page.locator(".modal-footer").getByRole("button", {
        name: "Squash and merge",
      }),
    ).toBeVisible();
    await expect(page.getByText("Create a merge commit")).toHaveCount(0);
    await expect(page.getByText("Rebase and merge")).toHaveCount(0);
  });
});

test.describe("detail load-error banner", () => {
  test("PR: failing route shows banner over the previous PR", async ({ page }) => {
    await mockApi(page);
    await mockSettings(page);

    await page.route(
      `**/api/v1/repos/${prA.repo_owner}/${prA.repo_name}/pulls/${prA.Number}`,
      async (route) => {
        if (route.request().method() === "GET") {
          await route.fulfill({
            status: 200,
            contentType: "application/json",
            body: JSON.stringify(detailEnvelopePR(prA)),
          });
          return;
        }
        await route.fallback();
      },
    );
    await page.route(
      `**/api/v1/repos/${prB.repo_owner}/${prB.repo_name}/pulls/${prB.Number}`,
      async (route) => {
        if (route.request().method() === "GET") {
          await route.fulfill({
            status: 500,
            contentType: "application/json",
            body: JSON.stringify({ detail: "upstream offline" }),
          });
          return;
        }
        await route.fallback();
      },
    );

    await page.goto(
      `/pulls/github/${prA.repo_owner}/${prA.repo_name}/${prA.Number}`,
    );
    await expect(page.locator(".detail-title")).toContainText(prA.Title);

    await page.evaluate(([owner, name, number]) => {
      window.history.pushState(
        null,
        "",
        `/pulls/github/${owner}/${name}/${number}`,
      );
      window.dispatchEvent(new PopStateEvent("popstate"));
    }, [prB.repo_owner, prB.repo_name, prB.Number] as const);

    const banner = page.getByTestId("detail-load-error");
    await expect(banner).toBeVisible();
    await expect(banner).toContainText("upstream offline");
    await expect(page.locator(".detail-title")).toContainText(prA.Title);
  });

  test("issue: failing route shows banner over the previous issue", async ({ page }) => {
    await mockApi(page);
    await mockSettings(page);

    await page.route(
      `**/api/v1/repos/${issueX.repo_owner}/${issueX.repo_name}/issues/${issueX.Number}`,
      async (route) => {
        if (route.request().method() === "GET") {
          await route.fulfill({
            status: 200,
            contentType: "application/json",
            body: JSON.stringify(detailEnvelopeIssue(issueX)),
          });
          return;
        }
        await route.fallback();
      },
    );
    await page.route(
      `**/api/v1/repos/${issueY.repo_owner}/${issueY.repo_name}/issues/${issueY.Number}`,
      async (route) => {
        if (route.request().method() === "GET") {
          await route.fulfill({
            status: 500,
            contentType: "application/json",
            body: JSON.stringify({ detail: "upstream offline" }),
          });
          return;
        }
        await route.fallback();
      },
    );

    await page.goto(
      `/issues/github/${issueX.repo_owner}/${issueX.repo_name}/${issueX.Number}`,
    );
    await expect(page.locator(".issue-detail .detail-title")).toContainText(
      issueX.Title,
    );

    await page.evaluate(([owner, name, number]) => {
      window.history.pushState(
        null,
        "",
        `/issues/github/${owner}/${name}/${number}`,
      );
      window.dispatchEvent(new PopStateEvent("popstate"));
    }, [issueY.repo_owner, issueY.repo_name, issueY.Number] as const);

    const banner = page.getByTestId("detail-load-error");
    await expect(banner).toBeVisible();
    await expect(banner).toContainText("upstream offline");
    await expect(page.locator(".issue-detail .detail-title")).toContainText(
      issueX.Title,
    );
  });
});

test.describe("PR detail stale-action gating", () => {
  test("close, comment, and create-workspace are inert while the new PR is loading", async ({ page }) => {
    const userMutations = recordUserMutations(page);
    const { release } = await setupHeldPR(page, prA, prB);

    await page.goto(`/pulls/github/${prA.repo_owner}/${prA.repo_name}/${prA.Number}`);
    await expect(page.locator(".detail-title")).toContainText(prA.Title);

    // Trigger an in-place navigation to the slow PR via popstate.
    await page.evaluate(([owner, name, number]) => {
      window.history.pushState(
        null,
        "",
        `/pulls/github/${owner}/${name}/${number}`,
      );
      window.dispatchEvent(new PopStateEvent("popstate"));
    }, [prB.repo_owner, prB.repo_name, prB.Number] as const);

    // PR A is still on screen because B is held.
    await expect(page.locator(".detail-title")).toContainText(prA.Title);

    // Close button must be disabled — clicking it must not fire
    // POST /github-state for PR B.
    const closeBtn = page.locator(".btn--close").first();
    await expect(closeBtn).toBeDisabled();
    await closeBtn.click({ force: true }).catch(() => {});

    // Create Workspace button must be disabled. A force-click must
    // not fire POST /workspaces.
    const createWs = page.locator("button.btn--workspace");
    await expect(createWs).toBeDisabled();
    await createWs.click({ force: true }).catch(() => {});

    // Comment submit: disabled.
    const commentSubmit = page
      .locator(".comment-box .submit-btn")
      .first();
    await expect(commentSubmit).toBeDisabled();

    // Release the slow load. PR B now displays and the controls
    // re-enable.
    release();
    await expect(page.locator(".detail-title")).toContainText(prB.Title);
    await expect(closeBtn).toBeEnabled();

    // No user-mutation request was sent during the stale window.
    expect(userMutations).toEqual([]);
  });
});

test.describe("issue detail stale-action gating", () => {
  test("star, close, comment, and create-workspace are inert while the new issue is loading", async ({ page }) => {
    const userMutations = recordUserMutations(page);
    const { release } = await setupHeldIssue(page, issueX, issueY);

    await page.goto(
      `/issues/github/${issueX.repo_owner}/${issueX.repo_name}/${issueX.Number}`,
    );
    await expect(page.locator(".issue-detail .detail-title")).toContainText(
      issueX.Title,
    );

    await page.evaluate(([owner, name, number]) => {
      window.history.pushState(
        null,
        "",
        `/issues/github/${owner}/${name}/${number}`,
      );
      window.dispatchEvent(new PopStateEvent("popstate"));
    }, [issueY.repo_owner, issueY.repo_name, issueY.Number] as const);

    // Issue X is still on screen because Y is held.
    await expect(page.locator(".issue-detail .detail-title")).toContainText(
      issueX.Title,
    );

    const starBtn = page.locator(".issue-detail .star-btn");
    await expect(starBtn).toBeDisabled();
    await starBtn.click({ force: true }).catch(() => {});

    const closeBtn = page.locator(".issue-detail .btn--close");
    await expect(closeBtn).toBeDisabled();
    await closeBtn.click({ force: true }).catch(() => {});

    const createWs = page
      .locator(".issue-detail button.btn--workspace");
    await expect(createWs).toBeDisabled();
    await createWs.click({ force: true }).catch(() => {});

    const commentSubmit = page
      .locator(".issue-detail .comment-box .submit-btn")
      .first();
    await expect(commentSubmit).toBeDisabled();

    release();
    await expect(page.locator(".issue-detail .detail-title")).toContainText(
      issueY.Title,
    );
    await expect(closeBtn).toBeEnabled();

    expect(userMutations).toEqual([]);
  });
});
