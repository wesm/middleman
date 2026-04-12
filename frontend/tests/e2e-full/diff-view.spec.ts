import { expect, test, type Page } from "@playwright/test";
import type { DiffFile, DiffLine, DiffResult, FilesResult } from "@middleman/ui/api/types";
import { acquireExclusiveLock } from "./support/exclusiveLock";

// --- Fixtures ---

// Small fixture: 4 files covering modified (multi-hunk), added, deleted, binary.
const smallDiff: DiffResult = {
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
  const files: DiffFile[] = [];
  for (let i = 0; i < 50; i++) {
    const lines: DiffLine[] = [];
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

async function mockDiffApi(page: Page, fixture: typeof smallDiff): Promise<void> {
  await page.route("**/api/v1/repos/acme/widgets/pulls/1/files", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(filesFromDiff(fixture)),
    });
  });
  await page.route("**/api/v1/repos/acme/widgets/pulls/1/diff*", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(fixture),
    });
  });
}

async function mockDiffApiError(page: Page, status: number, detail: string): Promise<void> {
  await page.route("**/api/v1/repos/acme/widgets/pulls/1/files", async (route) => {
    await route.fulfill({
      status,
      contentType: "application/json",
      body: JSON.stringify({ detail }),
    });
  });
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

async function waitForSidebarFilesLoaded(page: Page): Promise<void> {
  await page.locator(".diff-file-row").first()
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
    });
  });

  test("renders diff with sidebar file list, toolbar, and file diffs", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);
    await waitForSidebarFilesLoaded(page);

    // Sidebar inline file list shows all 4 files under the selected PR.
    await expect(page.locator(".diff-file-row")).toHaveCount(4);

    // Toolbar is visible.
    await expect(page.locator(".diff-toolbar")).toBeVisible();

    // All 4 diff file sections are rendered in the detail area.
    await expect(page.locator(".diff-file")).toHaveCount(4);
  });

  test("sidebar file list shows status indicators", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);
    await waitForSidebarFilesLoaded(page);

    // Files are grouped by directory in insertion order, each in its own group:
    //   internal/server/handler.go (M)
    //   frontend/src/lib/utils/format.ts (A)
    //   internal/legacy/old_handler.go (D)
    //   assets/logo.png (M, binary)
    const statuses = page.locator(".diff-file-row .diff-file-status");
    await expect(statuses.nth(0)).toHaveText("M");
    await expect(statuses.nth(1)).toHaveText("A");
    await expect(statuses.nth(2)).toHaveText("D");
    await expect(statuses.nth(3)).toHaveText("M");
  });

  test("sidebar shows directory headers for grouped files", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);
    await waitForSidebarFilesLoaded(page);

    // Each file lives in a different directory, so all 4 should render headers.
    const dirHeaders = page.locator(".diff-dir-header");
    await expect(dirHeaders).toHaveCount(4);
    await expect(dirHeaders.nth(0)).toHaveText("internal/server/");
    await expect(dirHeaders.nth(1)).toHaveText("frontend/src/lib/utils/");
    await expect(dirHeaders.nth(2)).toHaveText("internal/legacy/");
    await expect(dirHeaders.nth(3)).toHaveText("assets/");
  });

  test("clicking a sidebar file row highlights it as active", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);
    await waitForSidebarFilesLoaded(page);

    const secondRow = page.locator(".diff-file-row").nth(1);
    await secondRow.click();

    await expect(secondRow).toHaveClass(/diff-file-row--active/);
  });

  test("deleted file name has strikethrough in sidebar", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);
    await waitForSidebarFilesLoaded(page);

    // Third row is the deleted file (old_handler.go).
    const deletedName = page.locator(".diff-file-row").nth(2)
      .locator(".diff-file-name");
    await expect(deletedName).toHaveClass(/diff-file-name--deleted/);
  });

  test("detail tabs switch between conversation and files views", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);

    // On the /files route the "Files changed" tab is active.
    const filesTab = page.locator(".detail-tab", {
      hasText: "Files changed",
    });
    await expect(filesTab).toHaveClass(/detail-tab--active/);

    // Clicking "Conversation" navigates back to the PR detail.
    await page.locator(".detail-tab", {
      hasText: "Conversation",
    }).click();
    await expect(page).toHaveURL(/\/pulls\/acme\/widgets\/1$/);
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
    await page.route("**/api/v1/repos/acme/widgets/pulls/1/files", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(filesFromDiff(smallDiff)),
      });
    });
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

    // Wait for the re-fetch to land and assert it actually happened.
    await expect.poll(() => fetchCount).toBeGreaterThan(initialCount);
  });

  test("j/k keyboard navigation moves between files", async ({ page }) => {
    await mockDiffApi(page, smallDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);
    await waitForSidebarFilesLoaded(page);

    const rows = page.locator(".diff-file-row");

    // First file is active after initial load.
    await expect(rows.nth(0)).toHaveClass(/diff-file-row--active/);

    // Press j to move to next file.
    await page.keyboard.press("j");
    await expect(rows.nth(1)).toHaveClass(/diff-file-row--active/, { timeout: 2_000 });

    // Press j again.
    await page.keyboard.press("j");
    await expect(rows.nth(2)).toHaveClass(/diff-file-row--active/, { timeout: 2_000 });

    // Press k to move back.
    await page.keyboard.press("k");
    await expect(rows.nth(1)).toHaveClass(/diff-file-row--active/, { timeout: 2_000 });
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

  test("deleted file path has strikethrough styling in diff header", async ({ page }) => {
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
    });
  });

  test("large diff (50 files) renders all file headers within timeout", async ({ page }) => {
    await mockDiffApi(page, largeDiff);
    await navigateToDiff(page);
    await waitForDiffLoaded(page);

    // All 50 file headers should be in the DOM.
    await expect(page.locator(".diff-file .file-header")).toHaveCount(50, { timeout: 15_000 });

    // Sidebar inline file list should list all 50 files.
    await expect(page.locator(".diff-file-row")).toHaveCount(50);
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
    await page.route("**/api/v1/repos/acme/widgets/pulls/1/files", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(filesFromDiff(largeDiff)),
      });
    });
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
  test.describe.configure({ mode: "serial" });

  let releaseLock: (() => Promise<void>) | null = null;

  test.beforeAll(async () => {
    releaseLock = await acquireExclusiveLock("git-backed-diff");
  });

  test.afterAll(async () => {
    await releaseLock?.();
    releaseLock = null;
  });

  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.removeItem("diff-tab-width");
      localStorage.removeItem("diff-hide-whitespace");
      localStorage.removeItem("diff-collapsed-files");
    });
  });

  test("diff is not marked as stale", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/1/files");
    await page.locator(".diff-file").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    await expect(page.locator(".stale-banner")).not.toBeAttached();
  });

  test("real diff loads and renders all changed files", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/1/files");
    await page.locator(".diff-file").first()
      .waitFor({ state: "visible", timeout: 10_000 });
    await page.locator(".diff-file-row").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    // Should have 4 changed files from the test repo.
    await expect(page.locator(".diff-file")).toHaveCount(4);
    await expect(page.locator(".diff-file-row")).toHaveCount(4);
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

  test("added file shows A status in sidebar and only addition lines", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/1/files");
    await page.locator(".diff-file").first()
      .waitFor({ state: "visible", timeout: 10_000 });
    await page.locator(".diff-file-row").first()
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

    // Sidebar status should be "A".
    const cacheRow = page.locator(".diff-file-row", {
      hasText: "cache.go",
    });
    await expect(cacheRow.locator(".diff-file-status")).toHaveText("A");
  });

  test("deleted file shows D status in sidebar and only deletion lines", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/1/files");
    await page.locator(".diff-file").first()
      .waitFor({ state: "visible", timeout: 10_000 });
    await page.locator(".diff-file-row").first()
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

    // Sidebar status should be "D".
    const configRow = page.locator(".diff-file-row", {
      hasText: "config.yaml",
    });
    await expect(configRow.locator(".diff-file-status")).toHaveText("D");
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
    await expect(page.locator(".diff-file")).toHaveCount(3, { timeout: 10_000 });
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

  test("commit list uses UTC API values and local date rendering", async ({ page }) => {
    await page.addInitScript((offsetMs) => {
      const originalNow = Date.now.bind(Date);
      Date.now = () => originalNow() + offsetMs;
    }, 20 * 24 * 60 * 60 * 1000);

    await page.goto("/pulls/acme/widgets/1/files");
    await page.locator(".commit-section__toggle").click();
    await page.locator(".commit-item").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    const payload = await page.evaluate(async () => {
      const response = await fetch("/api/v1/repos/acme/widgets/pulls/1/commits");
      return response.json();
    });

    expect(payload.commits[0].authored_at).toMatch(/Z$/);

    const expectedLabel = await page.evaluate((iso: string) =>
      new Date(iso).toLocaleDateString(),
    payload.commits[0].authored_at);

    await expect(page.locator(".commit-item__date").first()).toHaveText(expectedLabel);
    expect(expectedLabel).not.toContain("T");
    expect(expectedLabel).not.toContain("Z");
  });
});
