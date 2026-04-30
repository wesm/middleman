import { expect, test, type Page } from "@playwright/test";

// Seeded data summary:
//   open PRs (8): widgets#1, #2, #6, #7, tools#1, tools#10, #11, #12 (last three form a stack)
//   closed/merged PRs (4): widgets#3 (merged), #4 (merged), #5 (closed), tools#2 (merged)

async function waitForPullList(page: Page): Promise<void> {
  // Wait for at least one PR item to appear (data loaded).
  await page
    .locator(".pull-item")
    .first()
    .waitFor({ state: "visible", timeout: 10_000 });
}

test.describe("PR list view", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/pulls");
    await waitForPullList(page);
  });

  test("renders open PRs by default with correct count", async ({ page }) => {
    const countBadge = page.locator(".filter-bar .list-count-chip");
    await expect(countBadge).toHaveText(/^8 PRs$/);
  });

  test("sidebar status pills use the shared chip component", async ({
    page,
  }) => {
    await expect(page.locator(".filter-bar .list-count-chip")).toHaveText(
      /^8 PRs$/,
    );

    // Seeded fixtures have no kanban_state rows; visiting a PR detail
    // creates the row server-side via EnsureKanbanState. Without this,
    // .status-chip never renders because PullItem hides it for empty
    // KanbanStatus.
    await page.locator(".pull-item").first().click();
    await page
      .locator(".pull-detail")
      .waitFor({ state: "visible", timeout: 5_000 });
    await page.goto("/pulls");
    await waitForPullList(page);

    await page.locator(".group-btn", { hasText: "All" }).click();
    const firstItem = page.locator(".pull-item").first();
    await expect(firstItem.locator(".repo-chip")).toBeVisible();
    await expect(firstItem.locator(".status-chip")).toBeVisible();
  });

  test("closed state shows closed and merged PRs with correct count", async ({
    page,
  }) => {
    await page.locator(".state-btn", { hasText: "Closed" }).click();

    const countBadge = page.locator(".filter-bar .list-count-chip");
    await expect(countBadge).toHaveText(/^4 PRs$/, { timeout: 5_000 });
  });

  test("search filters PRs by title", async ({ page }) => {
    const input = page.locator(".search-input");
    await input.fill("caching");

    // Wait for the count badge to reflect filtered results. The
    // matching item is already visible in the unfiltered list, so
    // we must wait on a condition that only becomes true after
    // the debounced search request completes.
    await expect(page.locator(".filter-bar .list-count-chip")).toHaveText(
      /^1 PRs?$/,
      { timeout: 5_000 },
    );

    // Verify the single remaining item is the expected one.
    const items = page.locator(".pull-item");
    await expect(items).toHaveCount(1);
    await expect(items.first().locator(".title")).toContainText(
      "caching layer",
    );
  });

  test("PR detail keeps the scrollbar on the pane edge", async ({ page }) => {
    await page
      .locator(".pull-item")
      .filter({ hasText: "caching layer" })
      .first()
      .click();

    const pullDetail = page.locator(".pull-detail");
    await expect(pullDetail).toBeVisible();

    await pullDetail.evaluate((el) => {
      const filler = document.createElement("div");
      filler.style.height = "3000px";
      filler.style.flexShrink = "0";
      filler.style.background = "transparent";
      filler.setAttribute("data-test-filler", "pull-scroll");
      el.appendChild(filler);
    });

    const overflowY = await pullDetail.evaluate(
      (el) => getComputedStyle(el).overflowY,
    );
    expect(["auto", "scroll"]).toContain(overflowY);

    const before = await pullDetail.evaluate((el) => ({
      scrollHeight: el.scrollHeight,
      clientHeight: el.clientHeight,
      scrollTop: el.scrollTop,
    }));
    expect(before.scrollHeight).toBeGreaterThan(before.clientHeight);
    expect(before.scrollTop).toBe(0);

    await pullDetail.evaluate((el) => {
      el.scrollTop = el.scrollHeight;
    });

    const finalScroll = await pullDetail.evaluate((el) => el.scrollTop);
    expect(finalScroll).toBeGreaterThan(0);

    const detailArea = page.locator(".main-area");
    const contentHeader = page.locator(".pull-detail .detail-header");
    const areaBox = await detailArea.boundingBox();
    const detailBox = await pullDetail.boundingBox();
    const headerBox = await contentHeader.boundingBox();
    expect(areaBox).not.toBeNull();
    expect(detailBox).not.toBeNull();
    expect(headerBox).not.toBeNull();
    if (areaBox !== null && detailBox !== null && headerBox !== null) {
      const scrollportWidth = await pullDetail.evaluate(
        (el) => el.clientWidth,
      );
      const scrollportCenter = detailBox.x + scrollportWidth / 2;
      const headerCenter = headerBox.x + headerBox.width / 2;
      expect(
        Math.abs(detailBox.x + detailBox.width - (areaBox.x + areaBox.width)),
      ).toBeLessThan(2);
      expect(Math.abs(headerCenter - scrollportCenter)).toBeLessThan(2);
      expect(headerBox.width).toBeLessThanOrEqual(800);
    }
  });
});
