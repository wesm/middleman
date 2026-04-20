import { expect, test } from "@playwright/test";

import { mockApi } from "./support/mockApi";

const testWorkspace = {
  id: "ws-123",
  platform_host: "github.com",
  repo_owner: "acme",
  repo_name: "widgets",
  item_type: "pull_request",
  item_number: 42,
  git_head_ref: "feature/auth",
  worktree_path: "/tmp/worktrees/ws-123",
  tmux_session: "middleman-ws-123",
  tmux_pane_title: null,
  tmux_working: false,
  status: "ready",
  created_at: "2026-04-10T12:00:00Z",
  mr_title: "Add auth middleware",
  mr_state: "open",
  mr_is_draft: false,
};

const testIssueWorkspace = {
  id: "ws-issue-7",
  platform_host: "github.com",
  repo_owner: "acme",
  repo_name: "widgets",
  item_type: "issue",
  item_number: 7,
  git_head_ref: "middleman/issue-7",
  worktree_path: "/tmp/worktrees/ws-issue-7",
  tmux_session: "middleman-ws-issue-7",
  status: "ready",
  created_at: "2026-04-10T12:00:00Z",
  mr_title: "Theme toggle does not stick",
  mr_state: "open",
};

const roborevRepos = {
  repos: [
    {
      name: "widgets",
      root_path: "/home/dev/widgets",
      count: 5,
    },
  ],
  total_count: 1,
};

const roborevJobs = {
  jobs: [
    {
      id: 1,
      status: "done",
      verdict: "pass",
      agent: "code-review",
      job_type: "review",
      git_ref: "abc12345",
      commit_subject: "Add auth middleware",
      enqueued_at: "2026-04-10T12:00:00Z",
      branch: "feature/auth",
      repo_name: "widgets",
      repo_id: 1,
      agentic: false,
      prompt_prebuilt: false,
      retry_count: 0,
    },
  ],
  has_more: false,
  stats: { done: 1, closed: 0, open: 0 },
};

const roborevStatus = {
  available: true,
  version: "0.52.0",
  endpoint: "http://127.0.0.1:17373",
  active_workers: 1,
  max_workers: 4,
  queued_jobs: 2,
  running_jobs: 1,
  completed_jobs: 5,
  failed_jobs: 0,
  canceled_jobs: 0,
};

/**
 * Mock all routes needed for terminal view tests.
 * Registers mockApi first (catch-all), then layers
 * workspace and roborev routes on top so they take
 * priority (Playwright uses LIFO route matching).
 */
async function setupTerminalMocks(
  page: import("@playwright/test").Page,
  opts?: {
    workspace?: typeof testWorkspace;
    roborevRepos?: typeof roborevRepos;
    roborevJobs?: typeof roborevJobs;
    roborevStatus?: typeof roborevStatus;
    workspaceDetailResponses?: Array<{
      status: number;
      body?: unknown;
    }>;
    workspaceRetryResponse?: {
      status: number;
      body?: unknown;
    };
  },
): Promise<void> {
  const ws = opts?.workspace ?? testWorkspace;
  const rrRepos = opts?.roborevRepos ?? roborevRepos;
  const rrJobs = opts?.roborevJobs ?? roborevJobs;
  const rrStatus = opts?.roborevStatus ?? roborevStatus;
  const detailResponses = [
    ...(opts?.workspaceDetailResponses ?? []),
  ];

  // Register catch-all first — later routes override.
  await mockApi(page);

  await page.route(
    "**/api/v1/events",
    async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body: "",
      });
    },
  );

  // Register list route first, then specific route.
  // Playwright uses LIFO matching, so the specific
  // /workspaces/:id registered last takes priority
  // over the list-only pattern.
  await page.route(
    "**/api/v1/workspaces",
    async (route) => {
      if (route.request().method() === "GET") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ workspaces: [ws] }),
        });
        return;
      }
      await route.fulfill({ status: 200 });
    },
  );

  await page.route(
    `**/api/v1/workspaces/${ws.id}/retry`,
    async (route) => {
      const response = opts?.workspaceRetryResponse ?? {
        status: 202,
        body: { ...ws, status: "creating" },
      };
      await route.fulfill({
        status: response.status,
        contentType: "application/json",
        body: JSON.stringify(response.body ?? {}),
      });
    },
  );

  await page.route(
    `**/api/v1/workspaces/${ws.id}`,
    async (route) => {
      if (route.request().method() === "GET") {
        const nextResponse = detailResponses.shift();
        if (nextResponse) {
          await route.fulfill({
            status: nextResponse.status,
            contentType: "application/json",
            body: JSON.stringify(
              nextResponse.body ?? {},
            ),
          });
          return;
        }
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(ws),
        });
        return;
      }
      // DELETE
      await route.fulfill({ status: 204 });
    },
  );

  // Route roborev API calls using a predicate to avoid
  // matching Vite module URLs like /@fs/.../api/roborev/...
  await page.route(
    (url) => url.pathname === "/api/v1/roborev/status",
    async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(rrStatus),
      });
    },
  );

  await page.route(
    (url) => url.pathname.startsWith("/api/roborev/"),
    async (route) => {
      const url = new URL(route.request().url());
      if (url.pathname.endsWith("/api/repos")) {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(rrRepos),
        });
        return;
      }
      if (
        url.pathname.endsWith("/api/jobs") ||
        url.pathname.includes("/api/jobs?")
      ) {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(rrJobs),
        });
        return;
      }
      if (url.pathname.endsWith("/status")) {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(rrStatus),
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

test(
  "roborev status mock ignores Vite module URLs",
  async ({ page }) => {
    await setupTerminalMocks(page);
    await page.goto("/");

    const response = await page.evaluate(async () => {
      const res = await fetch(
        "/@fs/tmp/project/api/v1/roborev/status",
      );
      return {
        status: res.status,
        body: await res.json(),
      };
    });

    expect(response).toEqual({
      status: 404,
      body: {
        error:
          "Unhandled GET /@fs/tmp/project/api/v1/roborev/status",
      },
    });
  },
);

test.describe("terminal state icons", () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.removeItem(
        "middleman-workspace-sidebar-tab",
      );
      localStorage.removeItem(
        "middleman-workspace-sidebar-open",
      );
      localStorage.removeItem(
        "middleman-workspace-sidebar-width",
      );
    });
  });

  test(
    "creating workspace shows spinner icon",
    async ({ page }) => {
      await setupTerminalMocks(page, {
        workspace: {
          ...testWorkspace,
          status: "creating",
        },
      });

      await page.goto("/terminal/ws-123");

      const stateMessage = page.locator(
        ".state-message",
      );
      await expect(stateMessage).toContainText(
        "Setting up workspace...",
      );
      await expect(
        stateMessage.locator(".spinner"),
      ).toBeVisible();
    },
  );

  test(
    "workspace load failure shows alert icon and retry recovers",
    async ({ page }) => {
      await setupTerminalMocks(page, {
        workspaceDetailResponses: [
          {
            status: 500,
            body: { error: "Internal error" },
          },
          {
            status: 200,
            body: testWorkspace,
          },
        ],
      });

      await page.goto("/terminal/ws-123");

      const stateMessage = page.locator(
        ".state-message.error",
      );
      await expect(stateMessage).toContainText(
        "Failed to load workspace (500)",
      );
      await expect(
        stateMessage.getByLabel(
          "Workspace load failed",
        ),
      ).toBeVisible();

      await stateMessage
        .getByRole("button", { name: "Retry" })
        .click();

      await expect(
        page.locator(".header-name"),
      ).toContainText("Add auth middleware");
    },
  );

  test(
    "workspace setup error retries setup and recovers",
    async ({ page }) => {
      let retryCalls = 0;
      await setupTerminalMocks(page, {
        workspaceDetailResponses: [
          {
            status: 200,
            body: {
              ...testWorkspace,
              status: "error",
              error_message:
                "tmux bootstrap failed",
            },
          },
          {
            status: 200,
            body: testWorkspace,
          },
        ],
        workspaceRetryResponse: {
          status: 202,
          body: { ...testWorkspace, status: "creating" },
        },
      });
      await page.route(
        "**/api/v1/workspaces/ws-123/retry",
        async (route) => {
          retryCalls += 1;
          await route.fulfill({
            status: 202,
            contentType: "application/json",
            body: JSON.stringify({
              ...testWorkspace,
              status: "creating",
            }),
          });
        },
      );

      await page.goto("/terminal/ws-123");

      const stateMessage = page.locator(
        ".state-message.error",
      );
      await expect(stateMessage).toContainText(
        "tmux bootstrap failed",
      );
      await expect(
        stateMessage.getByLabel(
          "Workspace setup failed",
        ),
      ).toBeVisible();

      await stateMessage
        .getByRole("button", { name: "Retry" })
        .click();

      expect(retryCalls).toBe(1);
      await expect(
        page.locator(".header-name"),
      ).toContainText("Add auth middleware");
    },
  );

  test(
    "workspace setup error can be deleted",
    async ({ page }) => {
      await setupTerminalMocks(page, {
        workspaceDetailResponses: [
          {
            status: 200,
            body: {
              ...testWorkspace,
              status: "error",
              error_message: "ensure clone failed",
            },
          },
        ],
      });

      await page.goto("/terminal/ws-123");

      const stateMessage = page.locator(
        ".state-message.error",
      );
      await expect(stateMessage).toContainText(
        "ensure clone failed",
      );

      await stateMessage
        .getByRole("button", { name: "Delete" })
        .click();

      await expect(page).toHaveURL(/\/pulls$/);
    },
  );
});

// -------------------------------------------------------
// Group 1: Toggle Behavior
// -------------------------------------------------------

test.describe("sidebar toggle behavior", () => {
  test.beforeEach(async ({ page }) => {
    // Clear any persisted sidebar state before each test.
    await page.addInitScript(() => {
      localStorage.removeItem(
        "middleman-workspace-list-sidebar-width",
      );
      localStorage.removeItem(
        "middleman-workspace-sidebar-tab",
      );
      localStorage.removeItem(
        "middleman-workspace-sidebar-open",
      );
      localStorage.removeItem(
        "middleman-workspace-sidebar-width",
      );
    });
    await setupTerminalMocks(page);
  });

  test(
    "workspace row shows working indicator with activity source",
    async ({ page }) => {
      await setupTerminalMocks(page, {
        workspace: {
          ...testWorkspace,
          tmux_pane_title: "⠴ t3code-b5014b03",
          tmux_working: true,
          tmux_activity_source: "title",
        },
      });

      await page.goto("/terminal/ws-123");

      const row = page.locator(".ws-row", {
        hasText: "Add auth middleware",
      });
      const badge = row.locator(".working-badge");
      await expect(badge).toBeVisible();
      await expect(badge).toContainText("Working");
      await expect(badge).toHaveAttribute(
        "title",
        "Working (title): ⠴ t3code-b5014b03",
      );
    },
  );

  test(
    "workspace list polls while mounted",
    async ({ page }) => {
      await setupTerminalMocks(page);
      let listRequests = 0;
      await page.route(
        "**/api/v1/workspaces",
        async (route) => {
          if (route.request().method() === "GET") {
            listRequests += 1;
            await route.fulfill({
              status: 200,
              contentType: "application/json",
              body: JSON.stringify({
                workspaces: [testWorkspace],
              }),
            });
            return;
          }
          await route.fulfill({ status: 200 });
        },
      );

      await page.goto("/terminal/ws-123");

      await expect
        .poll(() => listRequests)
        .toBeGreaterThanOrEqual(1);
      await expect
        .poll(() => listRequests, { timeout: 6500 })
        .toBeGreaterThanOrEqual(2);
    },
  );

  test(
    "workspace list resize reclamps the right sidebar",
    async ({ page }) => {
      await page.goto("/terminal/ws-123");

      const listSidebar = page.locator(
        ".workspace-list-sidebar",
      );
      await expect(listSidebar).toBeVisible();

      const prBtn = page.locator(".seg-btn", {
        hasText: "PR",
      });
      await prBtn.click();
      const rightSidebar = page.locator(".right-sidebar");
      await expect(rightSidebar).toBeVisible();

      const initialListWidth = await listSidebar.evaluate(
        (el) => el.getBoundingClientRect().width,
      );
      const initialRightSidebarWidth =
        await rightSidebar.evaluate(
          (el) => el.getBoundingClientRect().width,
        );

      const handle = page.getByRole("separator", {
        name: "Resize sidebar",
      });
      await expect(handle).toBeVisible();

      const box = await handle.boundingBox();
      expect(box).toBeTruthy();

      if (box) {
        await page.mouse.move(
          box.x + box.width / 2,
          box.y + box.height / 2,
        );
        await page.mouse.down();
        await page.mouse.move(
          box.x + 180,
          box.y + box.height / 2,
        );
        await page.mouse.up();
      }

      await expect
        .poll(async () =>
          rightSidebar.evaluate(
            (el) => el.getBoundingClientRect().width,
          ),
        )
        .toBeLessThan(initialRightSidebarWidth - 20);

      const resizedListWidth = await listSidebar.evaluate(
        (el) => el.getBoundingClientRect().width,
      );
      expect(resizedListWidth).toBeGreaterThan(
        initialListWidth + 100,
      );

      const terminalWidth = await page
        .locator(".terminal-area")
        .evaluate((el) => el.getBoundingClientRect().width);
      expect(terminalWidth).toBeGreaterThanOrEqual(
        300,
      );
    },
  );

  test(
    "segmented control visible in terminal header",
    async ({ page }) => {
      await page.goto("/terminal/ws-123");

      const segControl = page.locator(".seg-control");
      await expect(segControl).toBeVisible();
      await expect(
        segControl.locator(".seg-btn", { hasText: "PR" }),
      ).toBeVisible();
      await expect(
        segControl.locator(".seg-btn", {
          hasText: "Reviews",
        }),
      ).toBeVisible();
    },
  );

  test(
    "clicking PR segment opens sidebar with PR content",
    async ({ page }) => {
      await page.goto("/terminal/ws-123");

      const prBtn = page.locator(".seg-btn", {
        hasText: "PR",
      });
      await expect(prBtn).toBeVisible();
      await prBtn.click();

      // Sidebar should now be visible
      await expect(
        page.locator(".right-sidebar"),
      ).toBeVisible();
      // PR button should be active
      await expect(prBtn).toHaveClass(/active/);
    },
  );

  test(
    "clicking active segment closes sidebar",
    async ({ page }) => {
      await page.goto("/terminal/ws-123");

      const prBtn = page.locator(".seg-btn", {
        hasText: "PR",
      });
      // Open
      await prBtn.click();
      await expect(
        page.locator(".right-sidebar"),
      ).toBeVisible();

      // Click same segment again — should close
      await prBtn.click();
      await expect(
        page.locator(".right-sidebar"),
      ).toHaveCount(0);
      await expect(prBtn).not.toHaveClass(/active/);
    },
  );

  test(
    "clicking Reviews switches tab without closing",
    async ({ page }) => {
      await page.goto("/terminal/ws-123");

      const prBtn = page.locator(".seg-btn", {
        hasText: "PR",
      });
      const reviewsBtn = page.locator(".seg-btn", {
        hasText: "Reviews",
      });

      // Open PR tab
      await prBtn.click();
      await expect(
        page.locator(".right-sidebar"),
      ).toBeVisible();
      await expect(prBtn).toHaveClass(/active/);

      // Switch to Reviews
      await reviewsBtn.click();
      await expect(
        page.locator(".right-sidebar"),
      ).toBeVisible();
      await expect(reviewsBtn).toHaveClass(/active/);
      await expect(prBtn).not.toHaveClass(/active/);
    },
  );

  test(
    "Cmd+] toggles sidebar open and closed",
    async ({ page }) => {
      await page.goto("/terminal/ws-123");

      // Start closed
      await expect(
        page.locator(".right-sidebar"),
      ).toHaveCount(0);

      // Open via keyboard
      await page.keyboard.press("Meta+]");
      await expect(
        page.locator(".right-sidebar"),
      ).toBeVisible();

      // Close via keyboard
      await page.keyboard.press("Meta+]");
      await expect(
        page.locator(".right-sidebar"),
      ).toHaveCount(0);
    },
  );
});

// -------------------------------------------------------
// Group 2: Persistence
// -------------------------------------------------------

test.describe("sidebar persistence", () => {
  // Persistence tests reload the page, so we must NOT
  // use addInitScript (it re-runs on reload and would
  // clear the values we want to persist). Instead we
  // clear localStorage via evaluate after first goto.
  test.beforeEach(async ({ page }) => {
    await setupTerminalMocks(page);
  });

  async function clearSidebarStorage(
    page: import("@playwright/test").Page,
  ): Promise<void> {
    await page.evaluate(() => {
      localStorage.removeItem(
        "middleman-workspace-sidebar-tab",
      );
      localStorage.removeItem(
        "middleman-workspace-sidebar-open",
      );
      localStorage.removeItem(
        "middleman-workspace-sidebar-width",
      );
    });
  }

  test(
    "sidebar open state persists across reload",
    async ({ page }) => {
      await page.goto("/terminal/ws-123");
      await clearSidebarStorage(page);

      // Open sidebar
      const prBtn = page.locator(".seg-btn", {
        hasText: "PR",
      });
      await prBtn.click();
      await expect(
        page.locator(".right-sidebar"),
      ).toBeVisible();

      // Verify localStorage written
      const stored = await page.evaluate(() =>
        localStorage.getItem(
          "middleman-workspace-sidebar-open",
        ),
      );
      expect(stored).toBe("true");

      // Reload — sidebar should still be open
      await page.reload();
      await expect(
        page.locator(".right-sidebar"),
      ).toBeVisible();
    },
  );

  test(
    "sidebar tab persists across reload",
    async ({ page }) => {
      await page.goto("/terminal/ws-123");
      await clearSidebarStorage(page);

      // Open Reviews tab
      const reviewsBtn = page.locator(".seg-btn", {
        hasText: "Reviews",
      });
      await reviewsBtn.click();
      await expect(reviewsBtn).toHaveClass(/active/);

      // Verify localStorage
      const tab = await page.evaluate(() =>
        localStorage.getItem(
          "middleman-workspace-sidebar-tab",
        ),
      );
      expect(tab).toBe("reviews");

      // Reload
      await page.reload();
      const reviewsBtnAfter = page.locator(".seg-btn", {
        hasText: "Reviews",
      });
      await expect(reviewsBtnAfter).toHaveClass(/active/);
    },
  );

  test(
    "sidebar width persists after resize and reload",
    async ({ page }) => {
      await page.goto("/terminal/ws-123");
      await clearSidebarStorage(page);

      // Open sidebar
      await page
        .locator(".seg-btn", { hasText: "PR" })
        .click();
      await expect(
        page.locator(".right-sidebar"),
      ).toBeVisible();

      const handle = page.locator(
        ".sidebar-resize-handle",
      );
      const box = await handle.boundingBox();
      expect(box).toBeTruthy();

      if (box) {
        // Drag left to make sidebar wider
        await page.mouse.move(
          box.x + 2,
          box.y + box.height / 2,
        );
        await page.mouse.down();
        await page.mouse.move(
          box.x - 100,
          box.y + box.height / 2,
        );
        await page.mouse.up();
      }

      // Width should have increased from default 360
      const width = await page.evaluate(() =>
        localStorage.getItem(
          "middleman-workspace-sidebar-width",
        ),
      );
      expect(Number(width)).toBeGreaterThan(360);

      const savedWidth = Number(width);

      // Reload and check sidebar opens at saved width
      await page.reload();
      await expect(
        page.locator(".right-sidebar"),
      ).toBeVisible();

      const actualWidth = await page
        .locator(".right-sidebar")
        .evaluate((el) => el.offsetWidth);
      // Allow some tolerance for rounding
      expect(actualWidth).toBeGreaterThanOrEqual(
        savedWidth - 2,
      );
      expect(actualWidth).toBeLessThanOrEqual(
        savedWidth + 2,
      );
    },
  );
});

// -------------------------------------------------------
// Group 3: PR Tab
// -------------------------------------------------------

test.describe("sidebar PR tab", () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.removeItem(
        "middleman-workspace-sidebar-tab",
      );
      localStorage.removeItem(
        "middleman-workspace-sidebar-open",
      );
      localStorage.removeItem(
        "middleman-workspace-sidebar-width",
      );
    });
    await setupTerminalMocks(page);
  });

  test(
    "PR tab loads PR detail for workspace with linked PR",
    async ({ page }) => {
      await page.goto("/terminal/ws-123");

      // Open PR tab
      await page
        .locator(".seg-btn", { hasText: "PR" })
        .click();
      await expect(
        page.locator(".right-sidebar"),
      ).toBeVisible();

      // PR detail component should show PR title
      await expect(
        page.locator(
          ".right-sidebar .detail-title",
        ),
      ).toContainText("Add browser regression coverage");
    },
  );

  test(
    "PR tab shows empty state when item_number is 0",
    async ({ page }) => {
      const noLinkedPR = {
        ...testWorkspace,
        item_number: 0,
      };
      // Re-setup with modified workspace
      await setupTerminalMocks(page, {
        workspace: noLinkedPR,
      });

      await page.goto("/terminal/ws-123");

      // Open PR tab
      await page
        .locator(".seg-btn", { hasText: "PR" })
        .click();
      await expect(
        page.locator(".right-sidebar"),
      ).toBeVisible();

      await expect(
        page.locator(".right-sidebar .empty-state"),
      ).toContainText("No linked PR");
    },
  );
});

// -------------------------------------------------------
// Group 4: Issue Workspace Sidebar
// -------------------------------------------------------

test.describe("issue workspace sidebar", () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.removeItem(
        "middleman-workspace-sidebar-tab",
      );
      localStorage.removeItem(
        "middleman-workspace-sidebar-open",
      );
      localStorage.removeItem(
        "middleman-workspace-sidebar-width",
      );
    });
    await setupTerminalMocks(page, {
      workspace: testIssueWorkspace,
    });
  });

  test(
    "issue workspaces show an Issue segment instead of PR and Reviews",
    async ({ page }) => {
      await page.goto("/terminal/ws-issue-7");

      await expect(
        page.locator(".seg-btn", { hasText: "Issue" }),
      ).toBeVisible();
      await expect(
        page.locator(".seg-btn", { hasText: "PR" }),
      ).toHaveCount(0);
      await expect(
        page.locator(".seg-btn", { hasText: "Reviews" }),
      ).toHaveCount(0);
    },
  );

  test(
    "issue segment opens issue detail for issue-backed workspaces",
    async ({ page }) => {
      await page.goto("/terminal/ws-issue-7");

      await page
        .locator(".seg-btn", { hasText: "Issue" })
        .click();

      await expect(
        page.locator(".right-sidebar"),
      ).toBeVisible();
      await expect(
        page.locator(".right-sidebar .detail-title"),
      ).toContainText("Theme toggle does not stick");
    },
  );
});

// -------------------------------------------------------
// Group 5: Reviews Tab
// -------------------------------------------------------

test.describe("sidebar Reviews tab", () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.removeItem(
        "middleman-workspace-sidebar-tab",
      );
      localStorage.removeItem(
        "middleman-workspace-sidebar-open",
      );
      localStorage.removeItem(
        "middleman-workspace-sidebar-width",
      );
    });
  });

  test(
    "Reviews tab preserves a daemon version that already starts with v",
    async ({ page }) => {
      await setupTerminalMocks(page, {
        roborevStatus: {
          ...roborevStatus,
          version: "v0.52.0",
        },
      });
      await page.goto("/terminal/ws-123");

      await page
        .locator(".seg-btn", { hasText: "Reviews" })
        .click();
      await expect(
        page.locator(".right-sidebar"),
      ).toBeVisible();

      await expect(
        page.locator(
          '.right-sidebar .daemon-status [title="Daemon version"]',
        ),
      ).toHaveText("v0.52.0");
    },
  );

  test(
    "Reviews tab shows job list when roborev repo matches",
    async ({ page }) => {
      await setupTerminalMocks(page);
      await page.goto("/terminal/ws-123");

      // Open Reviews tab
      await page
        .locator(".seg-btn", { hasText: "Reviews" })
        .click();
      await expect(
        page.locator(".right-sidebar"),
      ).toBeVisible();

      // Job list should render the mock job
      await expect(
        page.locator(
          ".right-sidebar .job-row",
        ),
      ).toBeVisible();
      await expect(
        page.locator(".right-sidebar .job-row"),
      ).toContainText("Add auth middleware");
    },
  );

  test(
    "Reviews tab shows empty state when no repo matches",
    async ({ page }) => {
      await setupTerminalMocks(page, {
        roborevRepos: { repos: [], total_count: 0 },
      });
      await page.goto("/terminal/ws-123");

      // Open Reviews tab
      await page
        .locator(".seg-btn", { hasText: "Reviews" })
        .click();
      await expect(
        page.locator(".right-sidebar"),
      ).toBeVisible();

      // Should show empty/no-reviews message
      await expect(
        page.locator(".right-sidebar .empty-state"),
      ).toContainText("No reviews");
    },
  );

  test(
    "branch picker shows and clears branch filter",
    async ({ page }) => {
      await setupTerminalMocks(page);
      await page.goto("/terminal/ws-123");

      // Open Reviews tab
      await page
        .locator(".seg-btn", { hasText: "Reviews" })
        .click();
      await expect(
        page.locator(".right-sidebar"),
      ).toBeVisible();

      // Branch filter should show workspace branch
      const picker = page.locator(
        '.right-sidebar .picker-button[title="Filter by repository"]',
      );
      await expect(picker).toContainText("feature/auth");

      // Selecting All Repos clears the branch filter
      await picker.click();
      await page
        .locator(".right-sidebar .dropdown-item", {
          hasText: "All Repos",
        })
        .click();
      await expect(picker).toContainText("All Repos");
    },
  );

  test(
    "selecting a job does not navigate to /reviews",
    async ({ page }) => {
      await setupTerminalMocks(page);
      await page.goto("/terminal/ws-123");

      // Open Reviews tab
      await page
        .locator(".seg-btn", { hasText: "Reviews" })
        .click();
      await expect(
        page.locator(".right-sidebar .job-row"),
      ).toBeVisible();

      // Click the job row
      await page
        .locator(".right-sidebar .job-row")
        .first()
        .click();

      // URL should stay on /terminal, not navigate
      await expect(page).toHaveURL(/\/terminal\/ws-123/);
      // Job row should get selected state
      await expect(
        page
          .locator(".right-sidebar .job-row")
          .first(),
      ).toHaveClass(/selected/);
    },
  );
});
