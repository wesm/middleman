import { expect, test, type Page } from "@playwright/test";

async function openFirstPR(page: Page): Promise<void> {
  await page.goto("/pulls");
  await page.locator(".pull-item").first()
    .waitFor({ state: "visible", timeout: 10_000 });
  await page.locator(".pull-item").first().click();
  await page.locator(".pull-detail").waitFor({ state: "visible", timeout: 10_000 });
}

test.describe("PR detail branch info", () => {
  test("shows head and base branch in meta-row", async ({ page }) => {
    await openFirstPR(page);

    const metaBranch = page.locator(".meta-branch");
    await expect(metaBranch).toBeVisible();

    // Branch names render inside .branch-name spans
    const branchNames = metaBranch.locator(".branch-name");
    await expect(branchNames).toHaveCount(2);

    // Head branch (first) and base branch (second) should be non-empty
    await expect(branchNames.first()).not.toBeEmpty();
    await expect(branchNames.last()).not.toBeEmpty();

    // Arrow separator present
    const arrow = metaBranch.locator(".branch-arrow");
    await expect(arrow).toBeVisible();
  });

  test("branch names match seeded fixture data", async ({ page }) => {
    await openFirstPR(page);

    const branchNames = page.locator(".meta-branch .branch-name");

    // First seeded open PR (widgets#1) has HeadBranch: "feature/caching", BaseBranch: "main"
    await expect(branchNames.first()).toHaveText("feature/caching");
    await expect(branchNames.last()).toHaveText("main");
  });
});
