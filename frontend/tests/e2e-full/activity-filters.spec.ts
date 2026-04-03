import { expect, test, type Page } from "@playwright/test";

// The e2e server seeds the Activity config with view_mode: "flat"
// and time_range: "7d", so the flat table renders by default.
//
// 7d seeded data summary (14 items):
//   item_type: 8 PR, 6 issue
//   activity_type: 3 new_pr, 2 new_issue, 7 comment, 2 review, 0 commit
//   item_state: 13 open, 1 closed (issue#12)
//   bot authors: 2 (dependabot[bot] on PR#7 and issue#13)

async function waitForTable(page: Page): Promise<void> {
  await page.locator(".activity-table tbody .activity-row").first()
    .waitFor({ state: "visible", timeout: 10_000 });
}

// Click a filter button and wait for the /activity API response to
// complete, so assertions run against the updated DOM rather than
// stale pre-filter rows.
async function clickFilterAndWait(
  page: Page, label: string,
): Promise<void> {
  const responsePromise = page.waitForResponse(
    (r) => r.url().includes("/activity") && r.status() === 200,
  );
  await page.locator(".seg-btn", { hasText: label }).click();
  await responsePromise;
}

test.describe("activity feed filters", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
    await waitForTable(page);
  });

  test("PR filter shows only PR items", async ({ page }) => {
    await clickFilterAndWait(page, "PRs");

    const badges = page.locator(".activity-row .badge");
    const count = await badges.count();
    expect(count).toBeGreaterThan(0);

    for (let i = 0; i < count; i++) {
      const text = await badges.nth(i).textContent();
      expect(text?.trim()).toBe("PR");
    }
  });

  test("Issues filter shows only issue items", async ({ page }) => {
    await clickFilterAndWait(page, "Issues");

    const badges = page.locator(".activity-row .badge");
    const count = await badges.count();
    expect(count).toBeGreaterThan(0);

    for (let i = 0; i < count; i++) {
      const text = await badges.nth(i).textContent();
      expect(text?.trim()).toBe("Issue");
    }
  });

  test("All filter shows both PR and issue items", async ({ page }) => {
    // Start on PRs to change state, then switch to All.
    await clickFilterAndWait(page, "PRs");
    await clickFilterAndWait(page, "All");

    const badges = page.locator(".activity-row .badge");
    const allTexts: string[] = [];
    const count = await badges.count();
    for (let i = 0; i < count; i++) {
      const text = await badges.nth(i).textContent();
      allTexts.push(text?.trim() ?? "");
    }

    expect(allTexts).toContain("PR");
    expect(allTexts).toContain("Issue");
  });

  test("disabling Comments hides comment rows", async ({ page }) => {
    // Verify comments exist initially.
    const commentsBefore = page.locator(".evt-label.evt-comment");
    await expect(commentsBefore.first()).toBeVisible();
    const countBefore = await commentsBefore.count();
    expect(countBefore).toBeGreaterThan(0);

    // Open filter dropdown and disable Comments.
    await page.locator(".filter-btn").click();
    await page.locator(".filter-dropdown").waitFor({ state: "visible" });
    await page.locator(".filter-item", { hasText: "Comments" }).click();

    // Wait for the table to update (comments should disappear).
    await expect(
      page.locator(".evt-label.evt-comment"),
    ).toHaveCount(0, { timeout: 5_000 });
  });

  test("hide closed/merged removes those items", async ({ page }) => {
    // Verify closed/merged items exist initially.
    const closedBadges = page.locator(".state-badge.state-closed");
    const mergedBadges = page.locator(".state-badge.state-merged");
    const closedCount = await closedBadges.count();
    const mergedCount = await mergedBadges.count();
    expect(closedCount + mergedCount).toBeGreaterThan(0);

    // Open filter dropdown and enable "Hide closed/merged".
    await page.locator(".filter-btn").click();
    await page.locator(".filter-dropdown").waitFor({ state: "visible" });
    await page.locator(".filter-item", { hasText: "Hide closed/merged" })
      .click();

    // Both should now be gone.
    await expect(
      page.locator(".state-badge.state-closed"),
    ).toHaveCount(0, { timeout: 5_000 });
    await expect(
      page.locator(".state-badge.state-merged"),
    ).toHaveCount(0);
  });

  test("hide bots removes bot-authored items", async ({ page }) => {
    // Verify bot items exist (dependabot[bot]).
    const botCells = page.locator(
      ".activity-row .col-author",
      { hasText: "dependabot[bot]" },
    );
    const botCount = await botCells.count();
    expect(botCount).toBeGreaterThan(0);

    // Open filter dropdown and enable "Hide bots".
    await page.locator(".filter-btn").click();
    await page.locator(".filter-dropdown").waitFor({ state: "visible" });
    await page.locator(".filter-item", { hasText: "Hide bots" }).click();

    // Bot rows should disappear.
    await expect(
      page.locator(
        ".activity-row .col-author",
        { hasText: "dependabot[bot]" },
      ),
    ).toHaveCount(0, { timeout: 5_000 });
  });

  test("24h range shows fewer items than 7d", async ({ page }) => {
    const rows7d = page.locator(".activity-row");
    const count7d = await rows7d.count();
    expect(count7d).toBeGreaterThan(0);

    // Switch to 24h and wait for the API response.
    await clickFilterAndWait(page, "24h");

    // 24h has fewer items than 7d.
    const count24h = await page.locator(".activity-row").count();
    expect(count24h).toBeLessThan(count7d);
  });

  test("search filters by title", async ({ page }) => {
    const input = page.locator(".search-input");

    // Wait for the debounced search request to complete.
    const responsePromise = page.waitForResponse(
      (r) => r.url().includes("/activity")
        && r.url().includes("search") && r.status() === 200,
    );
    await input.fill("caching layer");
    await responsePromise;

    const rows = page.locator(".activity-row");
    const count = await rows.count();
    expect(count).toBeGreaterThan(0);

    // All visible rows should reference "Add widget caching layer".
    for (let i = 0; i < count; i++) {
      const title = await rows.nth(i).locator(".item-title").textContent();
      expect(title).toContain("Add widget caching layer");
    }
  });

  test("combined: PRs + hide closed/merged shows only open PRs",
    async ({ page }) => {
      // Click PRs filter and wait for API response.
      await clickFilterAndWait(page, "PRs");

      // Enable hide closed/merged (client-side filter, no API call).
      await page.locator(".filter-btn").click();
      await page.locator(".filter-dropdown").waitFor({ state: "visible" });
      await page.locator(".filter-item", { hasText: "Hide closed/merged" })
        .click();

      // Close the dropdown by clicking elsewhere.
      await page.locator(".controls-bar")
        .click({ position: { x: 5, y: 5 } });

      // Wait for merged/closed badges to disappear (client-side filter).
      await expect(
        page.locator(".state-badge.state-merged"),
      ).toHaveCount(0, { timeout: 5_000 });
      await expect(
        page.locator(".state-badge.state-closed"),
      ).toHaveCount(0);

      // Verify all badges are PR.
      const badges = page.locator(".activity-row .badge");
      const count = await badges.count();
      expect(count).toBeGreaterThan(0);

      for (let i = 0; i < count; i++) {
        const text = await badges.nth(i).textContent();
        expect(text?.trim()).toBe("PR");
      }
    },
  );
});
