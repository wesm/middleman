import { expect, test, type Page } from "@playwright/test";

// Seeded data summary:
//   open PRs (5): widgets#1, #2, #6, #7, tools#1
//   closed/merged PRs (4): widgets#3 (merged), #4 (merged), #5 (closed), tools#2 (merged)

async function waitForPullList(page: Page): Promise<void> {
  // Wait for at least one PR item to appear, or the empty state message.
  await page.locator(".pull-list").waitFor({ state: "visible", timeout: 10_000 });
  // Wait for the count badge to reflect loaded data (not "0 PRs" unless truly empty).
  await page.locator(".count-badge").waitFor({ state: "visible", timeout: 10_000 });
}

test.describe("PR list view", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/pulls");
    await waitForPullList(page);
  });

  test("renders open PRs by default with correct count", async ({ page }) => {
    const countBadge = page.locator(".count-badge");
    await expect(countBadge).toContainText("5", { timeout: 5_000 });
  });

  test("closed state shows closed and merged PRs with correct count", async ({ page }) => {
    // The "Closed" button switches to state=closed (includes merged).
    await page.locator(".state-btn", { hasText: "Closed" }).click();

    const countBadge = page.locator(".count-badge");
    await expect(countBadge).toContainText("4", { timeout: 5_000 });
  });

  test("search filters PRs by title", async ({ page }) => {
    const input = page.locator(".search-input");
    await input.fill("caching");

    // Debounce is 300ms + network request.
    await page.waitForTimeout(1_000);

    // Only "Add widget caching layer" should match.
    await expect(page.getByText("Add widget caching layer")).toBeVisible();
    // Other PRs should not be visible.
    await expect(page.getByText("Fix race condition in event loop")).not.toBeVisible();
    await expect(page.getByText("Add CLI flag parser")).not.toBeVisible();
  });
});
