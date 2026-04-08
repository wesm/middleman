import { expect, test, type Page } from "@playwright/test";

// Seed data repos: acme/widgets (most items) and acme/tools (fewer items).
// Open PRs (5): widgets#1, #2, #6, #7, tools#1   -> widgets 4, tools 1
// Open issues (4): widgets#10, #11, #13, tools#5 -> widgets 3, tools 1

async function waitForPullList(page: Page): Promise<void> {
  await page.locator(".pull-item").first()
    .waitFor({ state: "visible", timeout: 10_000 });
}

async function waitForIssueList(page: Page): Promise<void> {
  await page.locator(".issue-item").first()
    .waitFor({ state: "visible", timeout: 10_000 });
}

function widgetsHeader(page: Page) {
  return page.locator(".repo-header", { hasText: "acme/widgets" });
}

function toolsHeader(page: Page) {
  return page.locator(".repo-header", { hasText: "acme/tools" });
}

test.describe("collapsible repo groups", () => {
  test.beforeEach(async ({ page }) => {
    // Clear collapse state so every test starts expanded.
    // localStorage can only be touched after the first goto.
    await page.goto("/pulls");
    await page.evaluate(() => {
      localStorage.removeItem("middleman:collapsedRepos:pulls");
      localStorage.removeItem("middleman:collapsedRepos:issues");
    });
    await page.reload();
    await waitForPullList(page);
  });

  test("PR list — default expanded shows every PR and both headers", async ({ page }) => {
    await expect(widgetsHeader(page)).toBeVisible();
    await expect(toolsHeader(page)).toBeVisible();
    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "true");
    await expect(toolsHeader(page)).toHaveAttribute("aria-expanded", "true");
    await expect(page.locator(".pull-item")).toHaveCount(5);
  });

  test("PR list — collapsing acme/widgets hides its items, keeps header and count", async ({ page }) => {
    await widgetsHeader(page).click();

    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "false");
    await expect(widgetsHeader(page)).toBeVisible();
    await expect(
      widgetsHeader(page).locator(".repo-header__count"),
    ).toHaveText("4");

    // Only acme/tools' single PR remains visible.
    await expect(page.locator(".pull-item")).toHaveCount(1);
    // acme/tools stays expanded.
    await expect(toolsHeader(page)).toHaveAttribute("aria-expanded", "true");
  });

  test("PR list — expanding acme/widgets again restores its items", async ({ page }) => {
    await widgetsHeader(page).click();
    await expect(page.locator(".pull-item")).toHaveCount(1);

    await widgetsHeader(page).click();
    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "true");
    await expect(page.locator(".pull-item")).toHaveCount(5);
  });

  test("PR list — keyboard activation via Enter and Space toggles collapse", async ({ page }) => {
    // Focus the widgets header directly.
    await widgetsHeader(page).focus();
    await page.keyboard.press("Enter");
    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "false");
    await expect(page.locator(".pull-item")).toHaveCount(1);

    await widgetsHeader(page).focus();
    await page.keyboard.press("Space");
    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "true");
    await expect(page.locator(".pull-item")).toHaveCount(5);
  });

  test("PR list — collapse state persists across reload", async ({ page }) => {
    await widgetsHeader(page).click();
    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "false");

    await page.reload();
    await waitForPullList(page);

    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "false");
    await expect(page.locator(".pull-item")).toHaveCount(1);
  });

  test("collapse is independent across pulls and issues surfaces", async ({ page }) => {
    // Collapse acme/widgets on /pulls.
    await widgetsHeader(page).click();
    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "false");

    // Navigate to /issues — acme/widgets must still be expanded there.
    await page.goto("/issues");
    await waitForIssueList(page);

    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "true");
    // Seed data: widgets has 3 open issues, tools has 1 — total 4.
    await expect(page.locator(".issue-item")).toHaveCount(4);
  });

  test("issue list — collapse, expand, and persist acme/widgets", async ({ page }) => {
    await page.goto("/issues");
    await waitForIssueList(page);

    // Default: 4 issues total (3 widgets + 1 tools).
    await expect(page.locator(".issue-item")).toHaveCount(4);

    // Collapse widgets: 1 issue remains (tools#5).
    await widgetsHeader(page).click();
    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "false");
    await expect(
      widgetsHeader(page).locator(".repo-header__count"),
    ).toHaveText("3");
    await expect(page.locator(".issue-item")).toHaveCount(1);

    // Expand again: back to 4.
    await widgetsHeader(page).click();
    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "true");
    await expect(page.locator(".issue-item")).toHaveCount(4);

    // Collapse again and reload.
    await widgetsHeader(page).click();
    await page.reload();
    await waitForIssueList(page);
    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "false");
    await expect(page.locator(".issue-item")).toHaveCount(1);
  });
});
