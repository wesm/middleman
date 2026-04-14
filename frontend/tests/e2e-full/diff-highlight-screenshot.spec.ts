import { expect, test } from "@playwright/test";
import type { DiffResult, FilesResult } from "@middleman/ui/api/types";

// Fixture with long lines that force horizontal scroll.
const longLineDiff: DiffResult = {
  stale: false,
  whitespace_only_count: 0,
  files: [
    {
      path: ".github/workflows/ci.yml",
      old_path: ".github/workflows/ci.yml",
      status: "modified",
      is_binary: false,
      is_whitespace_only: false,
      additions: 8,
      deletions: 4,
      hunks: [
        {
          old_start: 10,
          old_count: 12,
          new_start: 10,
          new_count: 16,
          section: "jobs",
          lines: [
            { type: "context", content: "jobs:", old_num: 10, new_num: 10 },
            { type: "context", content: "  test:", old_num: 11, new_num: 11 },
            { type: "context", content: "    runs-on: ubuntu-latest", old_num: 12, new_num: 12 },
            { type: "delete", content: "    name: Run tests", old_num: 13 },
            { type: "add", content: "    name: Run tests with cross-browser coverage on multiple platforms and architectures", new_num: 13 },
            { type: "context", content: "    steps:", old_num: 14, new_num: 14 },
            { type: "delete", content: "      - run: go test ./...", old_num: 15 },
            { type: "add", content: "      - run: go build -o ./cmd/e2e-server/e2e-server ./cmd/e2e-server && playwright test --config playwright-e2e.config.ts --project=chromium --grep \"UTC timestamp\"", new_num: 15 },
            { type: "add", content: "      - run: playwright test --config playwright-e2e.config.ts --project=chromium --reporter=html --output=test-results/cross-browser-coverage", new_num: 16 },
            { type: "context", content: "  coverage:", old_num: 16, new_num: 17 },
            { type: "context", content: "    runs-on: ubuntu-latest", old_num: 17, new_num: 18 },
            { type: "delete", content: "      - run: go test -coverprofile=coverage.out ./...", old_num: 18 },
            { type: "delete", content: "      - run: go tool cover -html=coverage.out", old_num: 19 },
            { type: "add", content: "      - run: go test -coverprofile=coverage.out -covermode=atomic -race -shuffle=on -timeout=300s ./internal/... ./cmd/... 2>&1 | tee test-output.log", new_num: 19 },
            { type: "add", content: "      - run: go tool cover -html=coverage.out -o coverage-report.html && upload-artifact coverage-report.html coverage.out test-output.log", new_num: 20 },
            { type: "add", content: "      - run: playwright install --with-deps ${{ matrix.browser }} && playwright test --config playwright-e2e.config.ts --project=${{ matrix.browser }}", new_num: 21 },
            { type: "context", content: "    strategy:", old_num: 20, new_num: 22 },
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

test.describe("diff highlight backgrounds on horizontal scroll", () => {
  test("line backgrounds extend to full scroll width", async ({ page }) => {
    await page.route("**/api/v1/repos/acme/widgets/pulls/1/files", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(filesFromDiff(longLineDiff)),
      });
    });
    await page.route("**/api/v1/repos/acme/widgets/pulls/1/diff*", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(longLineDiff),
      });
    });

    await page.goto("/pulls/acme/widgets/1/files");
    await page.locator(".diff-file").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    // Wait for syntax highlighting to finish.
    await page.waitForTimeout(1000);

    // Screenshot before scroll.
    await page.screenshot({
      path: "test-results/diff-highlight-before-scroll.png",
      fullPage: false,
    });

    // Scroll the diff area to the right.
    const fileContent = page.locator(".file-content").first();
    await fileContent.evaluate((el) => { el.scrollLeft = 300; });
    await page.waitForTimeout(300);

    // Screenshot after scroll — backgrounds should be contiguous.
    await page.screenshot({
      path: "test-results/diff-highlight-after-scroll.png",
      fullPage: false,
    });

    // Verify add/delete lines have backgrounds that extend to the scroll width.
    // Check that the .file-rows wrapper is wider than .file-content's visible width.
    const widths = await fileContent.evaluate((el) => {
      const rows = el.querySelector(".file-rows");
      return {
        containerWidth: el.clientWidth,
        scrollWidth: el.scrollWidth,
        rowsWidth: rows ? rows.getBoundingClientRect().width : 0,
      };
    });

    // file-rows should be at least as wide as the scroll area (allow sub-pixel rounding).
    expect(widths.rowsWidth).toBeGreaterThanOrEqual(widths.scrollWidth - 1);
    // Scroll width should exceed container (long lines).
    expect(widths.scrollWidth).toBeGreaterThan(widths.containerWidth);
  });
});
