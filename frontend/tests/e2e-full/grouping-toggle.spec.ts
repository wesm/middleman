import { expect, test, type Page } from "@playwright/test";

// Seed data repos: acme/widgets (most items) and acme/tools (fewer items).
// Open PRs (8): widgets#1, #2, #6, #7, tools#1, #10, #11, #12 (last three form a stack)
// Open issues (4): widgets#10, #11, #13, tools#5

async function waitForPullList(page: Page): Promise<void> {
  await page.locator(".pull-item").first()
    .waitFor({ state: "visible", timeout: 10_000 });
}

async function waitForIssueList(page: Page): Promise<void> {
  await page.locator(".issue-item").first()
    .waitFor({ state: "visible", timeout: 10_000 });
}

test.describe("grouping toggle", () => {
  test.beforeEach(async ({ page }) => {
    // Clear localStorage to start with default (By Repo).
    await page.goto("/pulls");
    await page.evaluate(() => localStorage.removeItem("middleman:groupByRepo"));
    await page.reload();
    await waitForPullList(page);
  });

  test("PR list defaults to grouped with repo headers", async ({ page }) => {
    await expect(page.locator(".repo-header").first()).toBeVisible();
    // No repo badges visible in grouped mode.
    await expect(page.locator(".repo-badge")).toHaveCount(0);
  });

  test("PR list ungrouped shows repo badges and no headers", async ({ page }) => {
    // Click "All" in group toggle.
    await page.locator(".group-btn", { hasText: "All" }).click();

    // Repo headers should disappear.
    await expect(page.locator(".repo-header")).toHaveCount(0, { timeout: 5_000 });

    // Repo badges should appear on each item.
    const badges = page.locator(".repo-badge");
    await expect(badges.first()).toBeVisible();

    // Should have a badge for each PR.
    const items = page.locator(".pull-item");
    const itemCount = await items.count();
    await expect(badges).toHaveCount(itemCount);
  });

  test("toggle persists across page reload", async ({ page }) => {
    // Switch to ungrouped.
    await page.locator(".group-btn", { hasText: "All" }).click();
    await expect(page.locator(".repo-header")).toHaveCount(0, { timeout: 5_000 });

    // Reload the page.
    await page.reload();
    await waitForPullList(page);

    // Should still be ungrouped.
    await expect(page.locator(".repo-header")).toHaveCount(0);
    await expect(page.locator(".repo-badge").first()).toBeVisible();
  });

  test("toggle syncs from PRs to issues", async ({ page }) => {
    // Switch to ungrouped in PR list.
    await page.locator(".group-btn", { hasText: "All" }).click();
    await expect(page.locator(".repo-header")).toHaveCount(0, { timeout: 5_000 });

    // Navigate to issues.
    await page.goto("/issues");
    await waitForIssueList(page);

    // Issues should also be ungrouped.
    await expect(page.locator(".repo-header")).toHaveCount(0);
    await expect(page.locator(".repo-badge").first()).toBeVisible();
  });

  test("toggle syncs to activity threaded view", async ({ page }) => {
    // Switch to ungrouped in PR list.
    await page.locator(".group-btn", { hasText: "All" }).click();
    await expect(page.locator(".repo-header")).toHaveCount(0, { timeout: 5_000 });

    // Navigate to activity.
    await page.goto("/");

    // Switch to threaded mode.
    await page.locator(".seg-btn", { hasText: "Threaded" }).click();

    // Wait for threaded view to render.
    await page.locator(".threaded-view .item-row").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    // Repo headers should not be visible (ungrouped).
    await expect(page.locator(".threaded-view .repo-header")).toHaveCount(0);

    // Repo tags should appear on item rows.
    await expect(page.locator(".repo-tag").first()).toBeVisible();
  });

  test("activity threaded ungrouped keeps cross-repo items separate", async ({ page }) => {
    // Seed data has both widgets#1 and tools#1 as PRs.
    // In ungrouped threaded mode, they must remain separate threads.
    await page.goto("/");

    // Switch to threaded + ungrouped.
    await page.locator(".seg-btn", { hasText: "Threaded" }).click();
    await page.locator(".threaded-view .item-row").first()
      .waitFor({ state: "visible", timeout: 10_000 });
    // Click "All" in the grouping toggle (the segmented-control containing "By Repo").
    const groupControl = page.locator(".segmented-control")
      .filter({ has: page.locator(".seg-btn", { hasText: "By Repo" }) });
    await groupControl.locator(".seg-btn", { hasText: "All" }).click();

    // Wait for repo tags to appear (ungrouped).
    await page.locator(".repo-tag").first()
      .waitFor({ state: "visible", timeout: 5_000 });

    // Find all item rows whose ref is exactly "#1" (not #10, #11, etc.).
    const refOnes = page.locator(".item-row .item-ref").filter({ hasText: /^#1$/ });
    // There should be at least 2 (one for widgets, one for tools).
    const count = await refOnes.count();
    expect(count).toBeGreaterThanOrEqual(2);
  });

  test("activity toggle hidden in flat mode, visible in threaded", async ({ page }) => {
    await page.goto("/");

    // In flat mode (default), the By Repo / All toggle should not exist.
    // The flat/threaded control is visible; check there's no "By Repo" button.
    await page.locator(".seg-btn", { hasText: "Flat" })
      .waitFor({ state: "visible", timeout: 10_000 });
    await expect(page.locator(".seg-btn", { hasText: "By Repo" }))
      .toHaveCount(0);

    // Switch to threaded mode.
    await page.locator(".seg-btn", { hasText: "Threaded" }).click();
    await page.locator(".threaded-view").waitFor({ state: "visible", timeout: 10_000 });

    // Now By Repo / All toggle should be visible.
    await expect(page.locator(".seg-btn", { hasText: "By Repo" }))
      .toBeVisible();
  });

  test("threaded ungrouped empty state shows message", async ({ page }) => {
    await page.goto("/");

    // Search for a string that matches nothing to empty the result set.
    // Do this before switching to threaded so the debounced API call
    // completes while we're still in flat mode.
    const input = page.locator(".search-input");
    await input.fill("zzz_no_match_zzz");
    // Wait for the flat view to show the zero-results message
    // (not the loading placeholder) to prove the API returned 0 items.
    await expect(page.locator(".activity-feed .empty-state"))
      .toHaveText("No activity found", { timeout: 10_000 });

    // Now switch to threaded + ungrouped. ActivityThreaded receives [].
    await page.locator(".seg-btn", { hasText: "Threaded" }).click();
    // The "By Repo/All" toggle appears in threaded mode. Switch to ungrouped
    // by finding "All" within the same segmented-control as "By Repo".
    const groupControl = page.locator(".segmented-control")
      .filter({ has: page.locator(".seg-btn", { hasText: "By Repo" }) });
    await groupControl.locator(".seg-btn", { hasText: "All" }).click();

    // The threaded view's own empty state should render.
    await expect(page.locator(".threaded-view .empty-state"))
      .toBeVisible({ timeout: 5_000 });
  });

  test("j/k navigation follows flat order in ungrouped mode", async ({ page }) => {
    // Switch to ungrouped.
    await page.locator(".group-btn", { hasText: "All" }).click();
    await expect(page.locator(".repo-header")).toHaveCount(0, { timeout: 5_000 });

    // Capture the visible flat order of items.
    const allItems = page.locator(".pull-item");
    const firstVisibleMeta = await allItems.nth(0).locator(".meta-left").textContent();
    const secondVisibleMeta = await allItems.nth(1).locator(".meta-left").textContent();

    // Press j to select first item — should match the first visible item.
    await page.keyboard.press("j");
    await expect(page.locator(".pull-item.selected")).toHaveCount(1);
    const firstSelected = await page.locator(".pull-item.selected .meta-left").textContent();
    expect(firstSelected).toEqual(firstVisibleMeta);

    // Press j again — should match the second visible item.
    await page.keyboard.press("j");
    const secondSelected = await page.locator(".pull-item.selected .meta-left").textContent();
    expect(secondSelected).toEqual(secondVisibleMeta);
  });
});
