import { expect, test } from "@playwright/test";

import { mockApi } from "./support/mockApi";

test.beforeEach(async ({ page }) => {
  await mockApi(page);
});

async function mockPR(
  page: import("@playwright/test").Page,
  owner: string,
  name: string,
  number: number,
  title: string,
): Promise<void> {
  await page.route(
    `**/api/v1/repos/${owner}/${name}/pulls/${number}`,
    (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          merge_request: {
            ID: 1,
            RepoID: 1,
            GitHubID: 101,
            Number: number,
            URL: `https://github.com/${owner}/${name}/pull/${number}`,
            Title: title,
            Author: "marius",
            State: "open",
            IsDraft: false,
            Body: "Test PR body",
            HeadBranch: "feature/x",
            BaseBranch: "main",
            Additions: 10,
            Deletions: 2,
            CommentCount: 0,
            ReviewDecision: "",
            CIStatus: "success",
            CIChecksJSON: "[]",
            CreatedAt: "2026-04-10T12:00:00Z",
            UpdatedAt: "2026-04-10T12:00:00Z",
            LastActivityAt: "2026-04-10T12:00:00Z",
            MergedAt: null,
            ClosedAt: null,
            KanbanStatus: "new",
            Starred: false,
            repo_owner: owner,
            repo_name: name,
            platform_host: "github.com",
            worktree_links: [],
          },
          repo_owner: owner,
          repo_name: name,
          detail_loaded: true,
          detail_fetched_at: "2026-04-10T12:00:00Z",
          worktree_links: [],
        }),
      }),
  );
}

async function mockRoborev(
  page: import("@playwright/test").Page,
  rootPath: string | null,
  basePrefix: string = "",
): Promise<void> {
  // Match only the app-relative Roborev path at the configured
  // prefix. If WorkspaceSidebarView builds a malformed base (e.g.
  // "//api/roborev" from an unnormalized "/") or omits the subpath
  // prefix, the request will not match and the test will fail —
  // which is the intended regression guard.
  const apiPath = `${basePrefix}/api/roborev/`;
  await page.route(
    (url) => url.pathname.startsWith(apiPath),
    async (route) => {
      const url = new URL(route.request().url());
      if (url.pathname.endsWith("/api/repos")) {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            repos: rootPath
              ? [{ name: "widgets", root_path: rootPath, count: 0 }]
              : [],
            total_count: rootPath ? 1 : 0,
          }),
        });
        return;
      }
      if (url.pathname.includes("/api/jobs")) {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            jobs: [],
            has_more: false,
            stats: { done: 0, closed: 0, open: 0 },
          }),
        });
        return;
      }
      if (url.pathname.endsWith("/status")) {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ status: "ok" }),
        });
        return;
      }
      if (url.pathname.includes("/stream/events")) {
        await route.fulfill({
          status: 200,
          contentType: "text/event-stream",
          body: "",
        });
        return;
      }
      await route.fulfill({ status: 404 });
    },
  );
}

async function injectPrimaryHost(
  page: import("@playwright/test").Page,
): Promise<void> {
  await page.addInitScript(() => {
    window.__middleman_config = {
      embed: { activePlatformHost: "github.com" },
    };
  });
}

test(
  "sidebar route loads PR tab with full PullDetail",
  async ({ page }) => {
    await injectPrimaryHost(page);
    await mockPR(page, "acme", "widgets", 42, "Add thing");
    await page.goto(
      "/workspaces/sidebar/github.com/acme/widgets/42?branch=feat/x",
    );
    await expect(
      page.locator(".detail-title", { hasText: "Add thing" }),
    ).toBeVisible();
  },
);

test(
  "switching to Reviews tab resolves repo and renders JobTable",
  async ({ page }) => {
    await injectPrimaryHost(page);
    await mockPR(page, "acme", "widgets", 42, "Add thing");
    await mockRoborev(page, "/home/user/acme/widgets");
    await page.goto(
      "/workspaces/sidebar/github.com/acme/widgets/42?branch=feat/x",
    );
    await page
      .locator(".seg-btn", { hasText: "Reviews" })
      .click();
    await expect(page.getByText("No jobs found")).toBeVisible();
  },
);

test("?tab=reviews initializes on the Reviews tab", async ({ page }) => {
  await injectPrimaryHost(page);
  await mockPR(page, "acme", "widgets", 42, "Add thing");
  await mockRoborev(page, "/home/user/acme/widgets");
  await page.goto(
    "/workspaces/sidebar/github.com/acme/widgets/42?branch=feat/x&tab=reviews",
  );
  await expect(page.getByText("No jobs found")).toBeVisible();
});

test(
  "branch omitted with number > 0: Reviews empty, PR tab works",
  async ({ page }) => {
    await injectPrimaryHost(page);
    await mockPR(page, "acme", "widgets", 42, "Add thing");
    await mockRoborev(page, "/home/user/acme/widgets");
    await page.goto("/workspaces/sidebar/github.com/acme/widgets/42");
    await expect(
      page.locator(".detail-title", { hasText: "Add thing" }),
    ).toBeVisible();
    await page
      .locator(".seg-btn", { hasText: "Reviews" })
      .click();
    await expect(page.getByText("No jobs found")).toBeVisible();
  },
);

test(
  "number=0 renders 'No linked PR' on PR tab",
  async ({ page }) => {
    await injectPrimaryHost(page);
    await mockRoborev(page, "/home/user/acme/widgets");
    await page.goto("/workspaces/sidebar/github.com/acme/widgets/0");
    await expect(page.getByText("No linked PR")).toBeVisible();
  },
);

test(
  "startup state shows when activePlatformHost is null",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { activePlatformHost: null },
      };
    });
    await page.goto(
      "/workspaces/sidebar/github.com/acme/widgets/42?branch=feat/x",
    );
    await expect(page.getByTestId("startup-state")).toBeVisible();
  },
);

test(
  "URL tab param changes sync the active segment",
  async ({ page }) => {
    await injectPrimaryHost(page);
    await mockPR(page, "acme", "widgets", 42, "Add thing");
    await mockRoborev(page, "/home/user/acme/widgets");
    await page.goto(
      "/workspaces/sidebar/github.com/acme/widgets/42?branch=feat/x&tab=pr",
    );
    await expect(
      page.locator(".detail-title", { hasText: "Add thing" }),
    ).toBeVisible();
    const prBtn = page.locator(".seg-btn", { hasText: "PR" });
    const reviewsBtn = page.locator(".seg-btn", { hasText: "Reviews" });
    await expect(prBtn).toHaveClass(/active/);
    await expect(reviewsBtn).not.toHaveClass(/active/);

    await page.evaluate(() => {
      history.pushState(
        null,
        "",
        "/workspaces/sidebar/github.com/acme/widgets/42"
          + "?branch=feat/x&tab=reviews",
      );
      window.dispatchEvent(new PopStateEvent("popstate"));
    });

    await expect(reviewsBtn).toHaveClass(/active/);
    await expect(prBtn).not.toHaveClass(/active/);
    await expect(page.getByText("No jobs found")).toBeVisible();
  },
);

test(
  "non-primary host state shows 'Reveal in Host Settings'",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { activePlatformHost: "github.com" },
      };
    });
    await page.goto(
      "/workspaces/sidebar/example.com/acme/widgets/42?branch=feat/x",
    );
    await expect(page.getByTestId("non-primary-state")).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Reveal in Host Settings" }),
    ).toBeVisible();
  },
);

test(
  "clicking a segment pushes history so Back restores the previous tab",
  async ({ page }) => {
    await injectPrimaryHost(page);
    await mockPR(page, "acme", "widgets", 42, "Add thing");
    await mockRoborev(page, "/home/user/acme/widgets");
    await page.goto(
      "/workspaces/sidebar/github.com/acme/widgets/42?branch=feat/x&tab=pr",
    );
    const prBtn = page.locator(".seg-btn", { hasText: "PR" });
    const reviewsBtn = page.locator(".seg-btn", { hasText: "Reviews" });
    await expect(prBtn).toHaveClass(/active/);

    await reviewsBtn.click();
    await expect(reviewsBtn).toHaveClass(/active/);
    await expect(page).toHaveURL(/tab=reviews/);

    await page.goBack();
    await expect(prBtn).toHaveClass(/active/);
    await expect(page).toHaveURL(/tab=pr/);
  },
);

test(
  "sidebar route renders without global AppHeader or StatusBar",
  async ({ page }) => {
    await injectPrimaryHost(page);
    await mockPR(page, "acme", "widgets", 42, "Add thing");
    await mockRoborev(page, "/home/user/acme/widgets");
    await page.goto(
      "/workspaces/sidebar/github.com/acme/widgets/42?branch=feat/x",
    );
    await expect(
      page.locator(".detail-title", { hasText: "Add thing" }),
    ).toBeVisible();
    await expect(page.locator(".app-header")).toHaveCount(0);
    await expect(page.locator(".status-bar")).toHaveCount(0);
  },
);

test(
  "flash banner renders on sidebar route when a flash is triggered",
  async ({ page }) => {
    await injectPrimaryHost(page);
    await mockPR(page, "acme", "widgets", 42, "Add thing");
    await mockRoborev(page, "/home/user/acme/widgets");
    await page.goto(
      "/workspaces/sidebar/github.com/acme/widgets/42?branch=feat/x",
    );
    await expect(
      page.locator(".detail-title", { hasText: "Add thing" }),
    ).toBeVisible();
    await expect(page.locator(".flash-banner")).toHaveCount(0);

    await page.evaluate(async () => {
      const mod = await import(
        "/src/lib/stores/flash.svelte.ts"
      );
      mod.showFlash("Simulated sidebar error", 10_000);
    });

    await expect(page.locator(".flash-banner")).toBeVisible();
    await expect(
      page.locator(".flash-banner"),
    ).toContainText("Simulated sidebar error");
  },
);

test(
  "list-navigation keys do not navigate away from sidebar route",
  async ({ page }) => {
    await injectPrimaryHost(page);
    await mockPR(page, "acme", "widgets", 42, "Add thing");
    await mockRoborev(page, "/home/user/acme/widgets");
    await page.goto(
      "/workspaces/sidebar/github.com/acme/widgets/42?branch=feat/x",
    );
    await expect(
      page.locator(".detail-title", { hasText: "Add thing" }),
    ).toBeVisible();

    for (const key of ["j", "k", "Escape", "1", "2"]) {
      await page.keyboard.press(key);
    }

    const pathname = await page.evaluate(() => window.location.pathname);
    expect(pathname).toMatch(/^\/workspaces\/sidebar\//);
  },
);

test(
  "prefixed base path routes Roborev requests through the prefix",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__BASE_PATH__ = "/middleman/";
      window.__middleman_config = {
        embed: { activePlatformHost: "github.com" },
      };
    });
    const roborevUrls: string[] = [];
    page.on("request", (req) => {
      const url = new URL(req.url());
      if (req.resourceType() === "xhr" || req.resourceType() === "fetch") {
        if (url.pathname.includes("/api/roborev/")) {
          roborevUrls.push(url.pathname);
        }
      }
    });
    await page.route("**/middleman/api/v1/**", (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: "{}",
      }),
    );
    await mockRoborev(page, "/home/user/acme/widgets", "/middleman");

    await page.goto(
      "/middleman/workspaces/sidebar/github.com/acme/widgets/42"
        + "?branch=feat/x&tab=reviews",
    );
    await expect(page.getByText("No jobs found")).toBeVisible();

    expect(roborevUrls.length).toBeGreaterThan(0);
    for (const pathname of roborevUrls) {
      expect(pathname).toMatch(/^\/middleman\/api\/roborev\//);
    }
  },
);
