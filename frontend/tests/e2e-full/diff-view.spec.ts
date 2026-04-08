import { expect, test, type Page } from "@playwright/test";

// --- Fixtures ---

// Small fixture: 4 files covering modified (multi-hunk), added, deleted, binary.
const smallDiff = {
  stale: false,
  whitespace_only_count: 0,
  files: [
    {
      path: "internal/server/handler.go",
      old_path: "internal/server/handler.go",
      status: "modified",
      is_binary: false,
      is_whitespace_only: false,
      additions: 5,
      deletions: 2,
      hunks: [
        {
          old_start: 10,
          old_count: 7,
          new_start: 10,
          new_count: 8,
          section: "func handleRequest",
          lines: [
            { type: "context", content: "func handleRequest(w http.ResponseWriter, r *http.Request) {", old_num: 10, new_num: 10 },
            { type: "context", content: "\tctx := r.Context()", old_num: 11, new_num: 11 },
            { type: "delete", content: "\tlog.Println(\"handling request\")", old_num: 12 },
            { type: "add", content: "\tslog.Info(\"handling request\", \"method\", r.Method)", new_num: 12 },
            { type: "add", content: "\tslog.Info(\"request path\", \"path\", r.URL.Path)", new_num: 13 },
            { type: "context", content: "\tif err := process(ctx); err != nil {", old_num: 13, new_num: 14 },
            { type: "context", content: "\t\thttp.Error(w, err.Error(), 500)", old_num: 14, new_num: 15 },
          ],
        },
        {
          old_start: 30,
          old_count: 5,
          new_start: 31,
          new_count: 8,
          section: "func process",
          lines: [
            { type: "context", content: "func process(ctx context.Context) error {", old_num: 30, new_num: 31 },
            { type: "delete", content: "\treturn nil", old_num: 31 },
            { type: "add", content: "\tif err := validate(ctx); err != nil {", new_num: 32 },
            { type: "add", content: "\t\treturn fmt.Errorf(\"validation: %w\", err)", new_num: 33 },
            { type: "add", content: "\t}", new_num: 34 },
            { type: "add", content: "\treturn nil", new_num: 35 },
            { type: "context", content: "}", old_num: 32, new_num: 36 },
          ],
        },
      ],
    },
    {
      path: "frontend/src/lib/utils/format.ts",
      old_path: "frontend/src/lib/utils/format.ts",
      status: "added",
      is_binary: false,
      is_whitespace_only: false,
      additions: 8,
      deletions: 0,
      hunks: [
        {
          old_start: 0,
          old_count: 0,
          new_start: 1,
          new_count: 8,
          lines: [
            { type: "add", content: "export function formatDate(d: Date): string {", new_num: 1 },
            { type: "add", content: "  const year = d.getFullYear();", new_num: 2 },
            { type: "add", content: "  const month = String(d.getMonth() + 1).padStart(2, '0');", new_num: 3 },
            { type: "add", content: "  const day = String(d.getDate()).padStart(2, '0');", new_num: 4 },
            { type: "add", content: "  return `${year}-${month}-${day}`;", new_num: 5 },
            { type: "add", content: "}", new_num: 6 },
            { type: "add", content: "", new_num: 7 },
            { type: "add", content: "export function formatNumber(n: number): string {", new_num: 8 },
          ],
        },
      ],
    },
    {
      path: "internal/legacy/old_handler.go",
      old_path: "internal/legacy/old_handler.go",
      status: "deleted",
      is_binary: false,
      is_whitespace_only: false,
      additions: 0,
      deletions: 12,
      hunks: [
        {
          old_start: 1,
          old_count: 12,
          new_start: 0,
          new_count: 0,
          lines: [
            { type: "delete", content: "package legacy", old_num: 1 },
            { type: "delete", content: "", old_num: 2 },
            { type: "delete", content: "import \"net/http\"", old_num: 3 },
            { type: "delete", content: "", old_num: 4 },
            { type: "delete", content: "func OldHandler(w http.ResponseWriter, r *http.Request) {", old_num: 5 },
            { type: "delete", content: "\tw.WriteHeader(200)", old_num: 6 },
            { type: "delete", content: "\tw.Write([]byte(\"ok\"))", old_num: 7 },
            { type: "delete", content: "}", old_num: 8 },
            { type: "delete", content: "", old_num: 9 },
            { type: "delete", content: "func init() {", old_num: 10 },
            { type: "delete", content: "\thttp.HandleFunc(\"/old\", OldHandler)", old_num: 11 },
            { type: "delete", content: "}", old_num: 12 },
          ],
        },
      ],
    },
    {
      path: "assets/logo.png",
      old_path: "assets/logo.png",
      status: "modified",
      is_binary: true,
      is_whitespace_only: false,
      additions: 0,
      deletions: 0,
      hunks: [],
    },
  ],
};

// Generate a large diff (50 files) for perf tests.
function makeLargeDiff(): typeof smallDiff {
  const files = [];
  for (let i = 0; i < 50; i++) {
    const lines = [];
    for (let j = 1; j <= 20; j++) {
      if (j % 5 === 0) {
        lines.push({ type: "delete" as const, content: `  old line ${j}`, old_num: j });
        lines.push({ type: "add" as const, content: `  new line ${j}`, new_num: j });
      } else {
        lines.push({ type: "context" as const, content: `  line ${j}`, old_num: j, new_num: j });
      }
    }
    files.push({
      path: `src/pkg${Math.floor(i / 5)}/file_${i}.go`,
      old_path: `src/pkg${Math.floor(i / 5)}/file_${i}.go`,
      status: "modified",
      is_binary: false,
      is_whitespace_only: false,
      additions: 4,
      deletions: 4,
      hunks: [{ old_start: 1, old_count: 20, new_start: 1, new_count: 20, lines }],
    });
  }
  return { stale: false, whitespace_only_count: 0, files };
}

const largeDiff = makeLargeDiff();

// Stale fixture reuses small diff with stale flag.
const staleDiff = { ...smallDiff, stale: true };

// --- Helpers ---

async function mockDiffApi(page: Page, fixture: typeof smallDiff): Promise<void> {
  await page.route("**/api/v1/repos/acme/widgets/pulls/1/diff*", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(fixture),
    });
  });
}

async function mockDiffApiError(page: Page, status: number, detail: string): Promise<void> {
  await page.route("**/api/v1/repos/acme/widgets/pulls/1/diff*", async (route) => {
    await route.fulfill({
      status,
      contentType: "application/json",
      body: JSON.stringify({ detail }),
    });
  });
}

async function navigateToDiff(page: Page): Promise<void> {
  await page.goto("/pulls/acme/widgets/1/files");
}

async function waitForDiffLoaded(page: Page): Promise<void> {
  await page.locator(".diff-file").first()
    .waitFor({ state: "visible", timeout: 10_000 });
}

// --- Functional tests ---

test.describe("diff view", () => {
  test.beforeEach(async ({ page }) => {
    // Clear any persisted diff preferences so tests start clean.
    await page.addInitScript(() => {
      localStorage.removeItem("diff-tab-width");
      localStorage.removeItem("diff-hide-whitespace");
      localStorage.removeItem("diff-collapsed-files");
      localStorage.removeItem("diff-sidebar-width");
    });
  });

  test("renders diff with file tree, toolbar, and file diffs", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);

    // File tree is visible with the correct number of files.
    const fileTree = page.locator(".file-tree");
    await expect(fileTree).toBeVisible();
    const treeFiles = fileTree.locator(".tree-file");
    await expect(treeFiles).toHaveCount(4);

    // Toolbar is visible.
    await expect(page.locator(".diff-toolbar")).toBeVisible();

    // All 4 diff file sections are rendered.
    await expect(page.locator(".diff-file")).toHaveCount(4);
  });

  test("top bar shows PR title, file count, and stats", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);

    // PR title from seeded data.
    await expect(page.locator(".topbar-title"))
      .toHaveText("Add widget caching layer", { timeout: 5_000 });

    // File count and stats.
    const stats = page.locator(".topbar-stats");
    await expect(stats).toContainText("4 files");
    await expect(stats).toContainText("+13");
    await expect(stats).toContainText("-14");
  });

  test("file tree shows status badges", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);

    const badges = page.locator(".file-tree .file-badge");
    // M (modified handler.go), A (added format.ts), D (deleted old_handler.go), M (binary logo.png)
    await expect(badges.nth(0)).toHaveText("M");
    await expect(badges.nth(1)).toHaveText("A");
    await expect(badges.nth(2)).toHaveText("D");
    await expect(badges.nth(3)).toHaveText("M");
  });

  test("clicking a file in the tree highlights it as active", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);

    const secondFile = page.locator(".file-tree .tree-file").nth(1);
    await secondFile.click();

    await expect(secondFile).toHaveClass(/tree-file--active/);
  });

  test("file tree filter narrows the file list", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);

    const filterInput = page.locator(".file-tree .filter-input");
    await filterInput.fill("handler");

    // Only handler.go and old_handler.go should match.
    await expect(page.locator(".file-tree .tree-file")).toHaveCount(2);
  });

  test("file tree sidebar can be collapsed and expanded", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);

    // Collapse the sidebar.
    await page.locator(".collapse-btn").click();
    await expect(page.locator(".file-tree")).not.toBeAttached();
    await expect(page.locator(".sidebar-collapsed")).toBeVisible();

    // Expand it back.
    await page.locator(".expand-btn").click();
    await expect(page.locator(".file-tree")).toBeVisible();
  });

  test("clicking a file header collapses and expands its content", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);

    const firstFile = page.locator(".diff-file").first();
    const header = firstFile.locator(".file-header");
    const content = firstFile.locator(".file-content");

    // Content is initially visible.
    await expect(content).toBeVisible();

    // Collapse.
    await header.click();
    await expect(content).not.toBeAttached();

    // Expand.
    await header.click();
    await expect(content).toBeVisible();
  });

  test("toolbar tab width buttons change active state", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);

    // Default tab width is 4.
    const segments = page.locator(".diff-toolbar .segment");
    await expect(segments.nth(2)).toHaveClass(/segment--active/);

    // Click tab width 2.
    await segments.nth(1).click();
    await expect(segments.nth(1)).toHaveClass(/segment--active/);
    await expect(segments.nth(2)).not.toHaveClass(/segment--active/);
  });

  test("hide whitespace toggle triggers re-fetch", async ({ page }) => {
    let fetchCount = 0;
    await page.route("**/api/v1/repos/acme/widgets/pulls/1/diff*", async (route) => {
      fetchCount++;
      const url = new URL(route.request().url());
      const fixture = url.searchParams.get("whitespace") === "hide"
        ? { ...smallDiff, whitespace_only_count: 1 }
        : smallDiff;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(fixture),
      });
    });

    await navigateToDiff(page);
    await waitForDiffLoaded(page);
    const initialCount = fetchCount;

    // Toggle hide whitespace on.
    await page.locator(".toggle-switch").click();

    // Wait for the re-fetch to complete and the footer to appear.
    await expect(page.locator(".tree-footer"))
      .toContainText("1 whitespace-only file hidden", { timeout: 5_000 });
    expect(fetchCount).toBeGreaterThan(initialCount);
  });

  test("j/k keyboard navigation moves between files", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);

    // First file should be active by default.
    const treeFiles = page.locator(".file-tree .tree-file");
    await expect(treeFiles.nth(0)).toHaveClass(/tree-file--active/);

    // Press j to move to next file.
    await page.keyboard.press("j");
    await expect(treeFiles.nth(1)).toHaveClass(/tree-file--active/, { timeout: 2_000 });

    // Press j again.
    await page.keyboard.press("j");
    await expect(treeFiles.nth(2)).toHaveClass(/tree-file--active/, { timeout: 2_000 });

    // Press k to move back.
    await page.keyboard.press("k");
    await expect(treeFiles.nth(1)).toHaveClass(/tree-file--active/, { timeout: 2_000 });
  });

  test("back button navigates to PR detail via fallback path", async ({ page }) => {
    await mockDiffApi(page, smallDiff);

    // page.goto() doesn't set history.state.fromApp, so this tests the
    // fallback navigate() path in goBack(), not history.back().
    await page.goto("/pulls/acme/widgets/1/files");
    await waitForDiffLoaded(page);

    // Click back -- should navigate to the PR detail URL.
    await page.locator(".back-btn").click();
    await expect(page).toHaveURL(/\/pulls\/acme\/widgets\/1$/);
  });

  test("stale diff banner is shown when diff is stale", async ({ page }) => {
    await mockDiffApi(page, staleDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);

    await expect(page.locator(".stale-banner")).toBeVisible();
    await expect(page.locator(".stale-banner")).toContainText("outdated");
  });

  test("error state shown when diff API fails", async ({ page }) => {
    await mockDiffApiError(page, 404, "diff not available for this pull request");
    await navigateToDiff(page);

    await expect(page.locator(".diff-state-msg--error"))
      .toHaveText("diff not available for this pull request", { timeout: 10_000 });
  });

  test("diff content shows hunk headers and line types", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);

    const firstFile = page.locator(".diff-file").first();

    // Hunk headers.
    const hunkHeaders = firstFile.locator(".hunk-header");
    await expect(hunkHeaders).toHaveCount(2);
    await expect(hunkHeaders.first()).toContainText("@@ -10,7 +10,8 @@ func handleRequest");

    // Added lines (+ marker).
    const addedLines = firstFile.locator(".diff-line--add");
    await expect(addedLines.first()).toBeVisible();

    // Deleted lines (- marker).
    const deletedLines = firstFile.locator(".diff-line--del");
    await expect(deletedLines.first()).toBeVisible();
  });

  test("binary file shows notice instead of diff content", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);

    // Binary file is the 4th file (logo.png).
    const binaryFile = page.locator(".diff-file").nth(3);
    await expect(binaryFile.locator(".binary-notice")).toHaveText("Binary file changed");
  });

  test("deleted file path has strikethrough styling", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);

    // Deleted file is the 3rd file.
    const deletedHeader = page.locator(".diff-file").nth(2).locator(".file-path");
    await expect(deletedHeader).toHaveClass(/file-path--deleted/);
  });

  test("collapsed region shows unchanged line count between hunks", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);

    // First file (handler.go) has 2 hunks with a gap between them.
    // Hunk 1 ends at old line 14 (old_start=10, old_count=7 -> ends at 17),
    // Hunk 2 starts at old line 30 -> gap = 30 - 17 = 13 unchanged lines.
    const firstFile = page.locator(".diff-file").first();
    const collapsed = firstFile.locator(".collapsed-region");
    await expect(collapsed).toHaveCount(1);
    await expect(collapsed).toContainText("unchanged lines");
  });
});

// --- Perf tests ---

test.describe("diff view performance", () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.removeItem("diff-tab-width");
      localStorage.removeItem("diff-hide-whitespace");
      localStorage.removeItem("diff-collapsed-files");
      localStorage.removeItem("diff-sidebar-width");
    });
  });

  test("large diff (50 files) renders all file headers within timeout", async ({ page }) => {
    await mockDiffApi(page, largeDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);

    // All 50 file headers should be in the DOM.
    await expect(page.locator(".diff-file .file-header")).toHaveCount(50, { timeout: 15_000 });

    // File tree should list all 50 files.
    await expect(page.locator(".file-tree .tree-file")).toHaveCount(50);
  });

  test("collapsing a file removes its content from the DOM", async ({ page }) => {
    await mockDiffApi(page, largeDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);

    const firstFile = page.locator(".diff-file").first();

    // Content present before collapse.
    await expect(firstFile.locator(".file-content")).toBeAttached();

    // Collapse.
    await firstFile.locator(".file-header").click();
    await expect(firstFile.locator(".file-content")).not.toBeAttached();

    // Other files still have their content.
    await expect(page.locator(".diff-file").nth(1).locator(".file-content")).toBeAttached();
  });

  test("whitespace toggle on large diff completes re-render", async ({ page }) => {
    // Return fewer files when whitespace=hide so we can distinguish
    // the post-toggle render from the initial one.
    const hiddenDiff = { ...largeDiff, files: largeDiff.files.slice(0, 45) };
    await page.route("**/api/v1/repos/acme/widgets/pulls/1/diff*", async (route) => {
      const url = new URL(route.request().url());
      const fixture = url.searchParams.get("whitespace") === "hide"
        ? hiddenDiff
        : largeDiff;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(fixture),
      });
    });

    await navigateToDiff(page);
    await waitForDiffLoaded(page);

    // All 50 files present initially.
    await expect(page.locator(".diff-file .file-header")).toHaveCount(50, { timeout: 15_000 });

    // Toggle whitespace -- triggers a re-fetch with ?whitespace=hide
    // which returns fewer files.
    await page.locator(".toggle-switch").click();

    // Count should drop to 45, proving the re-fetch and re-render completed.
    await expect(page.locator(".diff-file .file-header")).toHaveCount(45, { timeout: 15_000 });
  });
});

// --- Git-backed tests (real diff pipeline, no route mocking) ---
// These use a real git repo created by testutil.SetupDiffRepo for
// acme/widgets PR #1. The diff contains:
//   - internal/handler.go: modified (2 hunks, log->slog + added line)
//   - internal/cache.go: added
//   - config.yaml: deleted
//   - README.md: whitespace-only change

test.describe("diff view (git-backed)", () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.removeItem("diff-tab-width");
      localStorage.removeItem("diff-hide-whitespace");
      localStorage.removeItem("diff-collapsed-files");
      localStorage.removeItem("diff-sidebar-width");
    });
  });

  test("real diff loads and renders all changed files", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/1/files");
    await page.locator(".diff-file").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    // Should have 4 changed files from the test repo.
    await expect(page.locator(".diff-file")).toHaveCount(4);
    await expect(page.locator(".file-tree .tree-file")).toHaveCount(4);
  });

  test("modified file has multiple hunks with correct content", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/1/files");
    await page.locator(".diff-file").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    // Find the handler.go file by its data attribute.
    const handlerFile = page.locator(
      '[data-file-path="internal/handler.go"]',
    );
    await expect(handlerFile).toBeVisible();

    // Should have 2 hunks (two separate modified regions).
    const hunks = handlerFile.locator(".hunk-header");
    await expect(hunks).toHaveCount(2);

    // Deleted line: old log.Println call.
    const deletedLines = handlerFile.locator(".diff-line--del");
    await expect(deletedLines.first()).toBeVisible();

    // Added line: new slog.Info call.
    const addedLines = handlerFile.locator(".diff-line--add");
    await expect(addedLines.first()).toBeVisible();

    // Verify actual diff content -- the old log import was replaced.
    await expect(handlerFile.locator(".diff-line--del .code").first())
      .toContainText("log");
    await expect(handlerFile.locator(".diff-line--add .code").first())
      .toContainText("slog");
  });

  test("added file shows A badge and only addition lines", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/1/files");
    await page.locator(".diff-file").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    const cacheFile = page.locator(
      '[data-file-path="internal/cache.go"]',
    );
    await expect(cacheFile).toBeVisible();

    // Only addition lines -- no deletions or context.
    const addedLines = cacheFile.locator(".diff-line--add");
    const deletedLines = cacheFile.locator(".diff-line--del");
    await expect(addedLines.first()).toBeVisible();
    await expect(deletedLines).toHaveCount(0);
    // No context lines in a pure-add file.
    const contextLines = cacheFile.locator(
      ".diff-line:not(.diff-line--add):not(.diff-line--del)",
    );
    await expect(contextLines).toHaveCount(0);

    // File tree badge should be "A".
    const treeBadge = page.locator(".file-tree .tree-file", {
      hasText: "cache.go",
    }).locator(".file-badge");
    await expect(treeBadge).toHaveText("A");
  });

  test("deleted file shows D badge and only deletion lines", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/1/files");
    await page.locator(".diff-file").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    const configFile = page.locator(
      '[data-file-path="config.yaml"]',
    );
    await expect(configFile).toBeVisible();

    // Only deletion lines -- no additions or context.
    const deletedLines = configFile.locator(".diff-line--del");
    const addedLines = configFile.locator(".diff-line--add");
    await expect(deletedLines.first()).toBeVisible();
    await expect(addedLines).toHaveCount(0);
    const contextLines = configFile.locator(
      ".diff-line:not(.diff-line--add):not(.diff-line--del)",
    );
    await expect(contextLines).toHaveCount(0);

    // File tree badge should be "D".
    const treeBadge = page.locator(".file-tree .tree-file", {
      hasText: "config.yaml",
    }).locator(".file-badge");
    await expect(treeBadge).toHaveText("D");
  });

  test("diff is not marked as stale", async ({ page }) => {
    // Fetch the diff API directly (not through the SPA) to verify
    // server-side staleness computation in isolation.
    const resp = await page.request.get(
      "/api/v1/repos/acme/widgets/pulls/1/diff",
    );
    const body = await resp.json();
    expect(body.stale).toBe(false);

    // Also verify the UI doesn't show the banner.
    await page.goto("/pulls/acme/widgets/1/files");
    await page.locator(".diff-file").first()
      .waitFor({ state: "visible", timeout: 10_000 });
    await expect(page.locator(".stale-banner")).not.toBeAttached();
  });

  test("top bar shows real file count and addition/deletion stats", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/1/files");
    await page.locator(".diff-file").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    const stats = page.locator(".topbar-stats");
    await expect(stats).toContainText("4 files");

    // Additions and deletions should be non-zero (from real git diff).
    const statsText = await stats.textContent();
    const addMatch = statsText?.match(/\+(\d+)/);
    const delMatch = statsText?.match(/-(\d+)/);
    expect(Number(addMatch?.[1])).toBeGreaterThan(0);
    expect(Number(delMatch?.[1])).toBeGreaterThan(0);
  });

  test("hide whitespace toggle filters whitespace-only files", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/1/files");
    await page.locator(".diff-file").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    // Initially 4 files visible.
    await expect(page.locator(".diff-file")).toHaveCount(4);

    // Toggle hide whitespace.
    await page.locator(".toggle-switch").click();

    // README.md is whitespace-only and should be hidden.
    // Wait for the re-fetch to complete.
    await expect(page.locator(".diff-file")).toHaveCount(3, { timeout: 10_000 });

    // Footer should indicate hidden file count.
    await expect(page.locator(".tree-footer"))
      .toContainText("1 whitespace-only file hidden", { timeout: 5_000 });
  });

  test("collapsed region appears between hunks in modified file", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/1/files");
    await page.locator(".diff-file").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    const handlerFile = page.locator(
      '[data-file-path="internal/handler.go"]',
    );

    // With 2 hunks separated by unchanged lines, there should be
    // a collapsed region between them.
    const collapsed = handlerFile.locator(".collapsed-region");
    await expect(collapsed).toHaveCount(1);
    await expect(collapsed).toContainText("unchanged lines");
  });
});
