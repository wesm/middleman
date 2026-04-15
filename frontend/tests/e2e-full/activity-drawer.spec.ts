import { expect, test, type Locator, type Page } from "@playwright/test";
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

// Multi-file diff fixture: 20 files with 20 lines each to force the
// diff area to overflow in the kanban drawer.
const multiFileDiff: DiffResult = {
  stale: false,
  whitespace_only_count: 0,
  files: Array.from({ length: 20 }, (_, i) => ({
    path: `src/file_${i}.go`,
    old_path: `src/file_${i}.go`,
    status: "modified" as const,
    is_binary: false,
    is_whitespace_only: false,
    additions: 10,
    deletions: 5,
    hunks: [
      {
        old_start: 1,
        old_count: 10,
        new_start: 1,
        new_count: 15,
        lines: Array.from({ length: 20 }, (_, j) => {
          const type
            = j % 3 === 0 ? "delete" : j % 3 === 1 ? "add" : "context";
          const line: {
            type: "delete" | "add" | "context";
            content: string;
            old_num?: number;
            new_num?: number;
          } = {
            type,
            content: `line ${j} of file ${i}`,
          };
          if (type !== "add") line.old_num = j + 1;
          if (type !== "delete") line.new_num = j + 1;
          return line;
        }),
      },
    ],
  })),
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

async function expectDiffFileVisibleInScrollArea(
  diffArea: Locator,
  filePath: string,
): Promise<void> {
  await expect.poll(async () => {
    return await diffArea.evaluate((container, path) => {
      const file = container.querySelector<HTMLElement>(
        `[data-file-path="${CSS.escape(path)}"]`,
      );
      if (!file) {
        return false;
      }

      const containerRect = container.getBoundingClientRect();
      const fileRect = file.getBoundingClientRect();
      return (
        fileRect.bottom > containerRect.top &&
        fileRect.top < containerRect.bottom
      );
    }, filePath);
  }).toBe(true);
}

test.describe("activity drawer", () => {
  test("PR drawer shows diff when switching to Files tab", async ({ page }) => {
    // Route-level mocks must be installed before navigation so the
    // diff store never sees a real backend response.
    await mockDiffForAllPRs(page, tinyDiff);

    await page.goto("/");
    await waitForActivityTable(page);

    // Click the activity row for acme/widgets#1 specifically. Match by
    // title text to avoid ordering dependencies and to verify the
    // drawer opens for the intended PR.
    const prRow = page
      .locator(".activity-row")
      .filter({ has: page.locator(".badge", { hasText: "PR" }) })
      .filter({ hasText: "Add widget caching layer" })
      .first();
    await prRow.click();

    // Drawer opens with the conversation tab by default.
    const drawer = page.locator(".drawer-panel");
    await expect(drawer).toBeVisible();

    // Drawer header shows owner/name#number for the clicked PR. This
    // catches a regression where the wrong PR would still open the
    // drawer.
    await expect(drawer.locator(".drawer-title")).toHaveText("acme/widgets#1");

    // Click the "Files changed" tab inside the drawer.
    await drawer.locator(".detail-tab", { hasText: "Files changed" }).click();

    // Diff is rendered inside the drawer.
    await expect(drawer.locator(".diff-view")).toBeVisible();
    await expect(drawer.locator(".diff-toolbar")).toBeVisible();
    await expect(drawer.locator(".diff-file")).toHaveCount(1);

    // Switching back to Conversation unmounts the diff and restores
    // the conversation view.
    await drawer.locator(".detail-tab", { hasText: "Conversation" }).click();
    await expect(drawer.locator(".diff-view")).toHaveCount(0);
    await expect(drawer.locator(".pull-detail")).toBeVisible();

    // Escape closes the drawer and the parent activity feed is
    // preserved underneath.
    await page.keyboard.press("Escape");
    await expect(drawer).toHaveCount(0);
    await expect(page.locator(".activity-table")).toBeVisible();
  });

  test("kanban drawer shows diff when switching to Files tab", async ({ page }) => {
    await mockDiffForAllPRs(page, tinyDiff);

    await page.goto("/pulls/board");
    await page.locator(".kanban-card").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    // Click the kanban card for widgets#1 specifically so the drawer
    // title assertion is deterministic.
    const card = page.locator(".kanban-card")
      .filter({ hasText: "Add widget caching layer" })
      .first();
    await card.click();

    const drawer = page.locator(".drawer-panel");
    await expect(drawer).toBeVisible();
    await expect(drawer.locator(".drawer-title")).toHaveText("acme/widgets#1");

    await drawer.locator(".detail-tab", { hasText: "Files changed" }).click();

    await expect(drawer.locator(".diff-view")).toBeVisible();
    await expect(drawer.locator(".diff-file")).toHaveCount(1);

    // Switching back to Conversation unmounts the diff and restores
    // the conversation view.
    await drawer.locator(".detail-tab", { hasText: "Conversation" }).click();
    await expect(drawer.locator(".diff-view")).toHaveCount(0);
    await expect(drawer.locator(".pull-detail")).toBeVisible();

    // Escape closes the drawer and the kanban board is preserved
    // underneath.
    await page.keyboard.press("Escape");
    await expect(drawer).toHaveCount(0);
    await expect(page.locator(".kanban-board")).toBeVisible();
  });

  test("activity drawer Files tab renders the file/commit sidebar", async ({ page }) => {
    // Regression guard: when the drawer is on the Files tab, the
    // left pane must include the same Commits section + file list
    // that the standalone PR browser shows. Without this, the
    // drawer's diff view loses navigability for multi-file PRs.
    await mockDiffForAllPRs(page, tinyDiff);

    await page.goto("/");
    await waitForActivityTable(page);

    const prRow = page
      .locator(".activity-row")
      .filter({ has: page.locator(".badge", { hasText: "PR" }) })
      .filter({ hasText: "Add widget caching layer" })
      .first();
    await prRow.click();

    const drawer = page.locator(".drawer-panel");
    await expect(drawer).toBeVisible();
    await drawer.locator(".detail-tab", { hasText: "Files changed" }).click();

    // The Files tab is a horizontal split: sidebar on the left,
    // diff view on the right.
    const sidebar = drawer.locator(".files-layout > .files-sidebar");
    await expect(sidebar).toBeVisible();
    await expect(drawer.locator(".files-layout > .files-main .diff-view")).toBeVisible();

    // Sidebar shows the collapsed Commits section header.
    await expect(sidebar.locator(".commit-section .commit-section__label"))
      .toHaveText("Commits");

    // Sidebar lists the diff files (one row per file in tinyDiff).
    await expect(sidebar.locator(".diff-file-row")).toHaveCount(1);
    await expect(sidebar.locator(".diff-file-row .diff-file-name"))
      .toHaveText("handler.go");
  });

  test("kanban drawer Files tab renders the file/commit sidebar", async ({ page }) => {
    await mockDiffForAllPRs(page, tinyDiff);

    await page.goto("/pulls/board");
    await page.locator(".kanban-card").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    const card = page.locator(".kanban-card")
      .filter({ hasText: "Add widget caching layer" })
      .first();
    await card.click();

    const drawer = page.locator(".drawer-panel");
    await expect(drawer).toBeVisible();
    await drawer.locator(".detail-tab", { hasText: "Files changed" }).click();

    const sidebar = drawer.locator(".files-layout > .files-sidebar");
    await expect(sidebar).toBeVisible();
    await expect(drawer.locator(".files-layout > .files-main .diff-view")).toBeVisible();

    await expect(sidebar.locator(".commit-section .commit-section__label"))
      .toHaveText("Commits");
    await expect(sidebar.locator(".diff-file-row")).toHaveCount(1);
    await expect(sidebar.locator(".diff-file-row .diff-file-name"))
      .toHaveText("handler.go");
  });

  test("activity drawer multi-file sidebar clicks navigate DiffView", async ({ page }) => {
    // Regression guard for the actual navigation bug behind the
    // sidebar addition: a multi-file PR must let the user click any
    // file row in the drawer sidebar and see DiffView focus on that
    // file. A broken DiffSidebar/DiffView hookup would still pass
    // the single-file render test.
    await mockDiffForAllPRs(page, multiFileDiff);

    await page.goto("/");
    await waitForActivityTable(page);

    const prRow = page
      .locator(".activity-row")
      .filter({ has: page.locator(".badge", { hasText: "PR" }) })
      .filter({ hasText: "Add widget caching layer" })
      .first();
    await prRow.click();

    const drawer = page.locator(".drawer-panel");
    await expect(drawer).toBeVisible();
    await drawer.locator(".detail-tab", { hasText: "Files changed" }).click();

    const sidebar = drawer.locator(".files-layout > .files-sidebar");
    const diffArea = drawer.locator(".files-layout > .files-main .diff-area");

    await expect(diffArea).toBeVisible();

    // Sidebar lists all 20 fixture files; the first one is active
    // by default.
    await expect(sidebar.locator(".diff-file-row")).toHaveCount(20);
    await expect(
      sidebar.locator(".diff-file-row.diff-file-row--active"),
    ).toHaveCount(1);
    await expect(
      sidebar.locator(".diff-file-row.diff-file-row--active .diff-file-name"),
    ).toHaveText("file_0.go");

    // Click a non-first file in the sidebar.
    await sidebar.locator(".diff-file-row", { hasText: "file_5.go" }).click();

    // The clicked row becomes the active row, and DiffView scrolls
    // the corresponding DiffFile into view.
    await expect(
      sidebar.locator(".diff-file-row.diff-file-row--active .diff-file-name"),
    ).toHaveText("file_5.go");
    await expectDiffFileVisibleInScrollArea(diffArea, "src/file_5.go");
  });

  test("drawer Files tab stacks sidebar on narrow viewports", async ({ page }) => {
    // Regression guard: below the responsive breakpoint the fixed
    // 280px sidebar would crush the diff pane. Verify the sidebar
    // stacks above the diff (flex-direction: column) so the diff
    // pane keeps a usable width.
    await page.setViewportSize({ width: 600, height: 800 });
    await mockDiffForAllPRs(page, tinyDiff);

    await page.goto("/");
    await waitForActivityTable(page);

    const prRow = page
      .locator(".activity-row")
      .filter({ has: page.locator(".badge", { hasText: "PR" }) })
      .filter({ hasText: "Add widget caching layer" })
      .first();
    await prRow.click();

    const drawer = page.locator(".drawer-panel");
    await expect(drawer).toBeVisible();
    await drawer.locator(".detail-tab", { hasText: "Files changed" }).click();

    const layout = drawer.locator(".files-layout");
    await expect(layout).toBeVisible();
    const direction = await layout.evaluate(
      (el) => getComputedStyle(el).flexDirection,
    );
    expect(direction).toBe("column");

    // Sidebar and diff pane are both visible, stacked vertically.
    const sidebar = drawer.locator(".files-layout > .files-sidebar");
    const main = drawer.locator(".files-layout > .files-main");
    await expect(sidebar).toBeVisible();
    await expect(main).toBeVisible();

    const sidebarBox = await sidebar.boundingBox();
    const mainBox = await main.boundingBox();
    expect(sidebarBox).not.toBeNull();
    expect(mainBox).not.toBeNull();
    // Sidebar sits above the diff pane in stacked layout.
    expect(sidebarBox!.y + sidebarBox!.height).toBeLessThanOrEqual(
      mainBox!.y + 1,
    );
    // Diff pane takes the full drawer width, not a narrow strip.
    const drawerBox = await drawer.boundingBox();
    expect(drawerBox).not.toBeNull();
    expect(mainBox!.width).toBeGreaterThan(drawerBox!.width - 10);
  });

  test("kanban drawer multi-file sidebar clicks navigate DiffView", async ({ page }) => {
    await mockDiffForAllPRs(page, multiFileDiff);

    await page.goto("/pulls/board");
    await page.locator(".kanban-card").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    const card = page.locator(".kanban-card")
      .filter({ hasText: "Add widget caching layer" })
      .first();
    await card.click();

    const drawer = page.locator(".drawer-panel");
    await expect(drawer).toBeVisible();
    await drawer.locator(".detail-tab", { hasText: "Files changed" }).click();

    const sidebar = drawer.locator(".files-layout > .files-sidebar");
    const diffArea = drawer.locator(".files-layout > .files-main .diff-area");

    await expect(diffArea).toBeVisible();
    await expect(sidebar.locator(".diff-file-row")).toHaveCount(20);

    // Click the 12th file (file_11.go) and verify navigation.
    await sidebar.locator(".diff-file-row", { hasText: "file_11.go" }).click();
    await expect(
      sidebar.locator(".diff-file-row.diff-file-row--active .diff-file-name"),
    ).toHaveText("file_11.go");
    await expectDiffFileVisibleInScrollArea(diffArea, "src/file_11.go");
  });

  test("issue drawer scrolls internally to bottom of content", async ({ page }) => {
    await page.goto("/");
    await waitForActivityTable(page);

    // Target the Safari bug issue (widgets#10) specifically for
    // deterministic selection.
    const issueRow = page
      .locator(".activity-row")
      .filter({ has: page.locator(".badge", { hasText: "Issue" }) })
      .filter({ hasText: "Safari" })
      .first();
    await issueRow.click();

    const drawer = page.locator(".drawer-panel");
    await expect(drawer).toBeVisible();
    await expect(drawer.locator(".drawer-title")).toHaveText("acme/widgets#10");

    // The issue-detail element exists inside the drawer.
    const issueDetail = drawer.locator(".issue-detail");
    await expect(issueDetail).toBeVisible();

    // Inject a tall filler so the content guarantees overflow. The
    // seeded issue body is short; this isolates the test from fixture
    // content and verifies the actual scroll container behavior.
    // flex-shrink: 0 is required because .issue-detail is a flex
    // column; without it, the child would be shrunk to fit.
    await issueDetail.evaluate((el) => {
      const filler = document.createElement("div");
      filler.style.height = "3000px";
      filler.style.flexShrink = "0";
      filler.style.background = "transparent";
      filler.setAttribute("data-test-filler", "scroll-test");
      el.appendChild(filler);
    });

    // Verify .issue-detail is the actual scroll container.
    const overflowY = await issueDetail.evaluate(
      (el) => getComputedStyle(el).overflowY,
    );
    expect(["auto", "scroll"]).toContain(overflowY);

    // Content now overflows and scroll starts at top.
    const before = await issueDetail.evaluate((el) => ({
      scrollHeight: el.scrollHeight,
      clientHeight: el.clientHeight,
      scrollTop: el.scrollTop,
    }));
    expect(before.scrollHeight).toBeGreaterThan(before.clientHeight);
    expect(before.scrollTop).toBe(0);

    // Scroll to bottom on the intended container.
    await issueDetail.evaluate((el) => {
      el.scrollTop = el.scrollHeight;
    });

    // Scroll position advanced from 0.
    const finalScroll = await issueDetail.evaluate((el) => el.scrollTop);
    expect(finalScroll).toBeGreaterThan(0);

    // The drawer itself should still be visible after the scroll action.
    await expect(drawer).toBeVisible();
  });

  test("activity drawer spans full viewport width", async ({ page }) => {
    await page.goto("/");
    await waitForActivityTable(page);

    const prRow = page
      .locator(".activity-row")
      .filter({ has: page.locator(".badge", { hasText: "PR" }) })
      .first();
    await prRow.click();

    const drawer = page.locator(".drawer-panel");
    await expect(drawer).toBeVisible();

    const viewport = page.viewportSize();
    const drawerBox = await drawer.boundingBox();
    expect(viewport).not.toBeNull();
    expect(drawerBox).not.toBeNull();
    // Drawer spans the full viewport width (Task 6 widened to 100%).
    expect(drawerBox!.width).toBe(viewport!.width);
  });

  test("kanban drawer spans full viewport width", async ({ page }) => {
    await page.goto("/pulls/board");
    await page.locator(".kanban-card").first()
      .waitFor({ state: "visible", timeout: 10_000 });
    await page.locator(".kanban-card").first().click();

    const drawer = page.locator(".drawer-panel");
    await expect(drawer).toBeVisible();

    const viewport = page.viewportSize();
    const drawerBox = await drawer.boundingBox();
    expect(viewport).not.toBeNull();
    expect(drawerBox).not.toBeNull();
    // Drawer spans the full viewport width (Task 8 widened to 100%).
    // Sub-pixel rounding from layout can yield a box width that differs
    // from the viewport by a fraction of a pixel, so allow 1px slack.
    expect(Math.abs(drawerBox!.width - viewport!.width)).toBeLessThan(1);
  });

  test("active tab visual state switches with selection", async ({ page }) => {
    await mockDiffForAllPRs(page, tinyDiff);
    await page.goto("/");
    await waitForActivityTable(page);

    const prRow = page
      .locator(".activity-row")
      .filter({ has: page.locator(".badge", { hasText: "PR" }) })
      .first();
    await prRow.click();

    const drawer = page.locator(".drawer-panel");
    await expect(drawer).toBeVisible();

    const conversationTab = drawer.locator(".detail-tab", { hasText: "Conversation" });
    const filesTab = drawer.locator(".detail-tab", { hasText: "Files changed" });

    // Conversation is active by default.
    await expect(conversationTab).toHaveClass(/detail-tab--active/);
    await expect(filesTab).not.toHaveClass(/detail-tab--active/);

    // Clicking Files shifts active state.
    await filesTab.click();
    await expect(filesTab).toHaveClass(/detail-tab--active/);
    await expect(conversationTab).not.toHaveClass(/detail-tab--active/);

    // Clicking back restores conversation as active.
    await conversationTab.click();
    await expect(conversationTab).toHaveClass(/detail-tab--active/);
    await expect(filesTab).not.toHaveClass(/detail-tab--active/);
  });

  test("Files changed tab renders inline additions/deletions chips", async ({ page }) => {
    await page.goto("/");
    await waitForActivityTable(page);

    // Target widgets#1 specifically (seeded Additions=240, Deletions=30)
    // so the chip values are exact.
    const prRow = page
      .locator(".activity-row")
      .filter({ has: page.locator(".badge", { hasText: "PR" }) })
      .filter({ hasText: "Add widget caching layer" })
      .first();
    await prRow.click();

    const drawer = page.locator(".drawer-panel");
    await expect(drawer).toBeVisible();

    const filesTab = drawer.locator(".detail-tab", { hasText: "Files changed" });
    await expect(filesTab).toBeVisible();

    // Assert exact values against the seeded DB: widgets#1 has
    // Additions=240 and Deletions=30.
    await expect(filesTab.locator(".files-stat--add")).toHaveText("+240");
    await expect(filesTab.locator(".files-stat--del")).toHaveText("-30");
  });

  test("Escape closes drawer from Files tab", async ({ page }) => {
    await mockDiffForAllPRs(page, tinyDiff);
    await page.goto("/");
    await waitForActivityTable(page);

    const prRow = page
      .locator(".activity-row")
      .filter({ has: page.locator(".badge", { hasText: "PR" }) })
      .first();
    await prRow.click();

    const drawer = page.locator(".drawer-panel");
    await expect(drawer).toBeVisible();

    // Switch to the Files tab and confirm the diff renders.
    await drawer.locator(".detail-tab", { hasText: "Files changed" }).click();
    await expect(drawer.locator(".diff-view")).toBeVisible();

    // Escape should still close the drawer, even from the Files tab state.
    await page.keyboard.press("Escape");
    await expect(drawer).toHaveCount(0);
  });

  test("clicking the drawer backdrop does not close the drawer", async ({ page }) => {
    await page.goto("/");
    await waitForActivityTable(page);

    const prRow = page
      .locator(".activity-row")
      .filter({ has: page.locator(".badge", { hasText: "PR" }) })
      .first();
    await prRow.click();

    const drawer = page.locator(".drawer-panel");
    await expect(drawer).toBeVisible();

    // At 100% width there is no exposed backdrop area visually. The
    // backdrop element still exists as a positional wrapper, but its
    // click handler was removed in Task 6. Dispatching a click event
    // directly on the backdrop bypasses z-order layering, so if a
    // handler were reintroduced the drawer would close here.
    await page.locator(".drawer-backdrop").dispatchEvent("click");

    // Drawer should still be visible.
    await expect(drawer).toBeVisible();
  });

  test("kanban drawer Files view remains scrollable at full width", async ({ page }) => {
    // Serve a multi-file diff so the diff area is guaranteed to
    // overflow the drawer.
    await mockDiffForAllPRs(page, multiFileDiff);

    await page.goto("/pulls/board");
    await page.locator(".kanban-card").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    const card = page.locator(".kanban-card")
      .filter({ hasText: "Add widget caching layer" })
      .first();
    await card.click();

    const drawer = page.locator(".drawer-panel");
    await expect(drawer).toBeVisible();

    // Open the Files tab.
    await drawer.locator(".detail-tab", { hasText: "Files changed" }).click();
    await expect(drawer.locator(".diff-view")).toBeVisible();

    // The diff-area inside the drawer is the internal scroll
    // container. Wait for all 20 seeded files to render before
    // measuring overflow.
    const diffArea = drawer.locator(".diff-area");
    await expect(diffArea).toBeVisible();
    await expect(drawer.locator(".diff-file")).toHaveCount(20);

    // Content overflows the viewport.
    const before = await diffArea.evaluate((el) => ({
      scrollHeight: el.scrollHeight,
      clientHeight: el.clientHeight,
      scrollTop: el.scrollTop,
    }));
    expect(before.scrollHeight).toBeGreaterThan(before.clientHeight);
    expect(before.scrollTop).toBe(0);

    // Drive a real scroll to the bottom on the diff area.
    await diffArea.evaluate((el) => {
      el.scrollTop = el.scrollHeight;
    });

    const finalScroll = await diffArea.evaluate((el) => el.scrollTop);
    expect(finalScroll).toBeGreaterThan(0);
  });

  test("kanban drawer Close action refreshes board with open filter", async ({ page }) => {
    // Fully synthetic /pulls?state=open response so the test does not
    // depend on the shared backend's mutable state. We mock ONLY the
    // open-filtered list endpoint — the exact path the kanban refreshes
    // through after the close action — and let every other /pulls
    // request fall through to the real backend. That way the close
    // refresh is the only thing that can change what the board shows.
    let pullsContainsWidgets1 = true;

    const widgets1Card = {
      ID: 1001,
      Number: 1,
      Title: "Add widget caching layer",
      Body: "",
      Author: "alice",
      AuthorDisplayName: "alice",
      State: "open",
      KanbanStatus: "new",
      IsDraft: false,
      Additions: 240,
      Deletions: 30,
      CreatedAt: new Date(Date.now() - 2 * 3_600_000).toISOString(),
      UpdatedAt: new Date(Date.now() - 2 * 3_600_000).toISOString(),
      URL: "https://github.com/acme/widgets/pull/1",
      CIStatus: "",
      ReviewDecision: "",
      MergeableState: "",
      Starred: false,
      CIChecksJSON: "",
      labels: [],
      repo_owner: "acme",
      repo_name: "widgets",
      worktree_links: [],
    };

    // Always-present card. Lets the test assert that the close
    // refresh removes only widgets#1, not unrelated open PRs.
    const otherCard = {
      ID: 1002,
      Number: 2,
      Title: "Refactor widget pipeline",
      Body: "",
      Author: "bob",
      AuthorDisplayName: "bob",
      State: "open",
      KanbanStatus: "reviewing",
      IsDraft: false,
      Additions: 80,
      Deletions: 12,
      CreatedAt: new Date(Date.now() - 4 * 3_600_000).toISOString(),
      UpdatedAt: new Date(Date.now() - 4 * 3_600_000).toISOString(),
      URL: "https://github.com/acme/widgets/pull/2",
      CIStatus: "",
      ReviewDecision: "",
      MergeableState: "",
      Starred: false,
      CIChecksJSON: "",
      labels: [],
      repo_owner: "acme",
      repo_name: "widgets",
      worktree_links: [],
    };

    const buildOpenPullsResponse = (): unknown[] => {
      if (!pullsContainsWidgets1) return [otherCard];
      return [widgets1Card, otherCard];
    };

    // Function predicate: intercept only the top-level
    // /api/v1/pulls?state=open list. Other /pulls* requests
    // (per-PR detail, files, diff) fall through to the real backend.
    await page.route(
      (url) =>
        url.pathname.endsWith("/api/v1/pulls")
        && url.searchParams.get("state") === "open",
      async (route) => {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(buildOpenPullsResponse()),
        });
      },
    );

    // Mock the state-change endpoint and flip the synthetic list on
    // success. The detail load goes through the real backend (which
    // still shows widgets#1 as open), but the kanban board reads only
    // from the mocked /pulls endpoint.
    await page.route(
      "**/api/v1/repos/*/*/pulls/*/github-state",
      async (route) => {
        pullsContainsWidgets1 = false;
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: "{}",
        });
      },
    );

    // Track calls to the open-filtered pulls endpoint that occur
    // after the close button is clicked. onPullsRefresh forwarding
    // must trigger at least one such call.
    let openPullsRequestsAfterClose = 0;
    let closeClicked = false;
    page.on("request", (req) => {
      const url = req.url();
      if (
        closeClicked
        && url.includes("/api/v1/pulls")
        && url.includes("state=open")
      ) {
        openPullsRequestsAfterClose++;
      }
    });

    await page.goto("/pulls/board");

    // Open widgets#1 in the kanban drawer.
    const card = page.locator(".kanban-card")
      .filter({ hasText: "Add widget caching layer" })
      .first();
    await expect(card).toBeVisible({ timeout: 10_000 });
    await card.click();

    const drawer = page.locator(".drawer-panel");
    await expect(drawer).toBeVisible();
    await expect(drawer.locator(".drawer-title")).toHaveText("acme/widgets#1");

    // Wait for the Close button inside the drawer's PullDetail.
    const closeBtn = drawer.locator("button.btn--close");
    await expect(closeBtn).toBeVisible();

    closeClicked = true;
    await closeBtn.click();

    // After the close succeeds, widgets#1 disappears from the kanban
    // board because the refetched synthetic /pulls?state=open list
    // omits it. Other open cards remain visible — proves the refresh
    // dropped only widgets#1, not unrelated entries.
    await expect(
      page.locator(".kanban-card").filter({ hasText: "Add widget caching layer" }),
    ).toHaveCount(0, { timeout: 10_000 });
    await expect(
      page.locator(".kanban-card").filter({ hasText: "Refactor widget pipeline" }),
    ).toBeVisible();

    // At least one /api/v1/pulls?state=open request must have
    // happened after the close was clicked. This proves the refresh
    // went through the open-filtered path wired via onPullsRefresh.
    expect(openPullsRequestsAfterClose).toBeGreaterThan(0);
  });
});

test.describe("PR list tabs", () => {
  test("outer PR-list tab bar remains singular and router-driven", async ({ page }) => {
    // Mock the diff so navigating to /files does not depend on real data.
    await mockDiffForAllPRs(page, tinyDiff);

    await page.goto("/pulls/acme/widgets/1");

    // Wait for the PRListView tab bar (scoped to .detail-area) to
    // render.
    await page.locator(".detail-area .detail-tabs").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    // Exactly one tab bar is present inside the outer PRListView
    // container. If PullDetail ever stops respecting hideTabs, a
    // second .detail-tabs element would show up inside .detail-area.
    await expect(page.locator(".detail-area .detail-tabs")).toHaveCount(1);
    await expect(
      page.locator(
        ".detail-area .detail-tabs .detail-tab",
        { hasText: "Conversation" },
      ),
    ).toHaveCount(1);
    await expect(
      page.locator(
        ".detail-area .detail-tabs .detail-tab",
        { hasText: "Files changed" },
      ),
    ).toHaveCount(1);

    // Clicking Files changed in the outer tab bar updates the URL to
    // the /files sub-route.
    await page.locator(
      ".detail-area .detail-tabs .detail-tab",
      { hasText: "Files changed" },
    ).click();
    await expect(page).toHaveURL(/\/pulls\/acme\/widgets\/1\/files$/);
    await expect(page.locator(".diff-view")).toBeVisible();
    await expect(page.locator(".detail-area .detail-tabs")).toHaveCount(1);

    // Clicking Conversation routes back and keeps the tab bar
    // singular.
    await page.locator(
      ".detail-area .detail-tabs .detail-tab",
      { hasText: "Conversation" },
    ).click();
    await expect(page).toHaveURL(/\/pulls\/acme\/widgets\/1$/);
    await expect(page.locator(".detail-area .detail-tabs")).toHaveCount(1);
  });

  test("direct load of /pulls/:owner/:name/:number/files renders the diff with a single tab bar", async ({ page }) => {
    // Regression guard for initialization-only bugs that affect deep
    // links to the /files sub-route. Going there directly must render
    // the diff and keep exactly one outer PRListView tab bar — the
    // router-click test above does not exercise this path.
    await mockDiffForAllPRs(page, tinyDiff);

    await page.goto("/pulls/acme/widgets/1/files");

    await page.locator(".detail-area .detail-tabs").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    await expect(page.locator(".detail-area .detail-tabs")).toHaveCount(1);
    await expect(page.locator(".diff-view")).toBeVisible();
    await expect(
      page.locator(
        ".detail-area .detail-tabs .detail-tab--active",
        { hasText: "Files changed" },
      ),
    ).toHaveCount(1);
  });
});
