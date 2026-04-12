import { expect, test, type Page } from "@playwright/test";
import type { DiffResult, FilesResult } from "@middleman/ui/api/types";

// Minimal diff fixture: one modified file.
const tinyDiff: DiffResult = {
  stale: false,
  whitespace_only_count: 0,
  files: [
    {
      path: "src/handler.go",
      old_path: "src/handler.go",
      status: "modified",
      is_binary: false,
      is_whitespace_only: false,
      additions: 2,
      deletions: 1,
      hunks: [
        {
          old_start: 1,
          old_count: 3,
          new_start: 1,
          new_count: 4,
          lines: [
            { type: "context", content: "package main", old_num: 1, new_num: 1 },
            { type: "delete", content: "// old", old_num: 2 },
            { type: "add", content: "// new", new_num: 2 },
            { type: "add", content: "// added", new_num: 3 },
            { type: "context", content: "", old_num: 3, new_num: 4 },
          ],
        },
      ],
    },
  ],
};

function filesFromDiff(fixture: DiffResult): FilesResult {
  return {
    stale: fixture.stale,
    files: fixture.files.map((f) => ({
      ...f,
      additions: 0,
      deletions: 0,
      hunks: [],
    })),
  };
}

// Broad wildcard mock: any PR in any repo returns the same tiny diff.
// The activity feed test clicks "the first PR row", which could be any
// PR from the seeded fixtures; a wildcard mock keeps the test
// deterministic regardless of which PR is clicked.
async function mockDiffForAllPRs(
  page: Page, fixture: DiffResult,
): Promise<void> {
  await page.route("**/api/v1/repos/*/*/pulls/*/files", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(filesFromDiff(fixture)),
    });
  });
  await page.route("**/api/v1/repos/*/*/pulls/*/diff*", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(fixture),
    });
  });
}

async function waitForActivityTable(page: Page): Promise<void> {
  await page.locator(".activity-table tbody .activity-row").first()
    .waitFor({ state: "visible", timeout: 10_000 });
}

test.describe("activity drawer", () => {
  test("PR drawer shows diff when switching to Files tab", async ({ page }) => {
    // Route-level mocks must be installed before navigation so the
    // diff store never sees a real backend response.
    await mockDiffForAllPRs(page, tinyDiff);

    await page.goto("/");
    await waitForActivityTable(page);

    // Click the first PR activity row. The seeded activity feed contains
    // both PRs and issues; pick the first row tagged "PR". The wildcard
    // mock covers whichever PR this turns out to be.
    const prRow = page
      .locator(".activity-row")
      .filter({ has: page.locator(".badge", { hasText: "PR" }) })
      .first();
    await prRow.click();

    // Drawer opens with the conversation tab by default.
    const drawer = page.locator(".drawer-panel");
    await expect(drawer).toBeVisible();

    // Click the "Files changed" tab inside the drawer.
    await drawer.locator(".detail-tab", { hasText: "Files changed" }).click();

    // Diff is rendered inside the drawer.
    await expect(drawer.locator(".diff-view")).toBeVisible();
    await expect(drawer.locator(".diff-toolbar")).toBeVisible();
    await expect(drawer.locator(".diff-file")).toHaveCount(1);

    // Switching back to Conversation unmounts the diff.
    await drawer.locator(".detail-tab", { hasText: "Conversation" }).click();
    await expect(drawer.locator(".diff-view")).toHaveCount(0);

    // Escape closes the drawer.
    await page.keyboard.press("Escape");
    await expect(drawer).toHaveCount(0);
  });

  test("kanban drawer shows diff when switching to Files tab", async ({ page }) => {
    await mockDiffForAllPRs(page, tinyDiff);

    await page.goto("/pulls/board");
    await page.locator(".kanban-card").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    // Click the first kanban card in any column. The wildcard diff mock
    // covers whichever PR number this card represents.
    await page.locator(".kanban-card").first().click();

    // Kanban drawer (distinct from DetailDrawer) uses .drawer as its panel.
    const drawer = page.locator(".kanban-wrap .drawer");
    await expect(drawer).toBeVisible();

    await drawer.locator(".detail-tab", { hasText: "Files changed" }).click();

    await expect(drawer.locator(".diff-view")).toBeVisible();
    await expect(drawer.locator(".diff-file")).toHaveCount(1);

    await page.keyboard.press("Escape");
    await expect(drawer).toHaveCount(0);
  });

  test("issue drawer scrolls internally to bottom of content", async ({ page }) => {
    await page.goto("/");
    await waitForActivityTable(page);

    // Pick the first issue activity row.
    const issueRow = page
      .locator(".activity-row")
      .filter({ has: page.locator(".badge", { hasText: "Issue" }) })
      .first();
    await issueRow.click();

    const drawer = page.locator(".drawer-panel");
    await expect(drawer).toBeVisible();

    // The issue-detail element exists inside the drawer.
    const issueDetail = drawer.locator(".issue-detail");
    await expect(issueDetail).toBeVisible();

    // Force a scroll to the bottom. If scroll ownership is broken, the
    // walker finds no scroll container and the test fails.
    const result = await issueDetail.evaluate((el) => {
      // Walk up to find the scroll container (could be the element itself
      // or an ancestor). Use Element (not HTMLElement) since scroll props
      // are defined on the Element interface.
      let target: Element | null = el;
      while (target) {
        const style = getComputedStyle(target);
        if (style.overflowY === "auto" || style.overflowY === "scroll") {
          target.scrollTop = target.scrollHeight;
          return { found: true };
        }
        target = target.parentElement;
      }
      return { found: false };
    });

    expect(result.found).toBe(true);

    // The drawer itself should still be visible after the scroll action.
    await expect(drawer).toBeVisible();
  });
});

test.describe("PR list tabs", () => {
  test("only one tab bar with Conversation and Files changed tabs", async ({ page }) => {
    // Mock the diff so navigating to /files does not depend on real data.
    await mockDiffForAllPRs(page, tinyDiff);

    await page.goto("/pulls/acme/widgets/1");

    // Wait for the tab bar to render.
    await page.locator(".detail-tabs").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    // Assert exactly one tab bar is present. If PullDetail ever stops
    // respecting hideTabs, there would be 2 .detail-tabs containers
    // (PRListView's external bar + PullDetail's internal bar).
    await expect(page.locator(".detail-tabs")).toHaveCount(1);

    // And exactly one of each tab button (matched by the tab-specific
    // .detail-tab class so we don't pick up unrelated buttons that
    // happen to contain the same text — e.g., the current pull-detail
    // files-changed-btn).
    await expect(
      page.locator(".detail-tab", { hasText: "Conversation" }),
    ).toHaveCount(1);
    await expect(
      page.locator(".detail-tab", { hasText: "Files changed" }),
    ).toHaveCount(1);

    // Same guards for the files route.
    await page.goto("/pulls/acme/widgets/1/files");
    await page.locator(".diff-view").waitFor({ state: "visible", timeout: 10_000 });
    await expect(page.locator(".detail-tabs")).toHaveCount(1);
    await expect(
      page.locator(".detail-tab", { hasText: "Conversation" }),
    ).toHaveCount(1);
    await expect(
      page.locator(".detail-tab", { hasText: "Files changed" }),
    ).toHaveCount(1);
  });
});
