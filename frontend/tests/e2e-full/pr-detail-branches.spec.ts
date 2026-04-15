import { expect, test } from "@playwright/test";

test.describe("PR detail branch info", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/pulls/acme/widgets/1");
    await page.locator(".pull-detail")
      .waitFor({ state: "visible", timeout: 10_000 });
  });

  test("shows head and base branch buttons", async ({ page }) => {
    const metaBranch = page.locator(".meta-branch");
    await expect(metaBranch).toBeVisible();

    const branchBtns = metaBranch.locator(".branch-name-btn");
    await expect(branchBtns).toHaveCount(2);
    await expect(branchBtns.first()).not.toBeEmpty();
    await expect(branchBtns.last()).not.toBeEmpty();

    const arrow = metaBranch.locator(".branch-arrow");
    await expect(arrow).toBeVisible();
  });

  test("click branch shows copied feedback", async ({
    page, context, browserName,
  }) => {
    if (browserName === "chromium") {
      await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    }

    const headBtn = page.locator(
      ".meta-branch .branch-name-btn",
    ).first();
    await expect(headBtn).toHaveAttribute(
      "title", "Click to copy",
    );

    await headBtn.click();

    await expect(headBtn).toHaveClass(/branch-name-btn--copied/);
    await expect(headBtn).toHaveAttribute("title", "Copied!");
  });
});
