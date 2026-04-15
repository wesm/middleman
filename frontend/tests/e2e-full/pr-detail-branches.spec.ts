import { expect, test } from "@playwright/test";

test.describe("PR detail branch info", () => {
  test("shows head and base branch in meta-row", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/1");
    await page.locator(".pull-detail")
      .waitFor({ state: "visible", timeout: 10_000 });

    const metaBranch = page.locator(".meta-branch");
    await expect(metaBranch).toBeVisible();

    const branchBtns = metaBranch.locator(".branch-name-btn");
    await expect(branchBtns).toHaveCount(2);

    await expect(branchBtns.first()).not.toBeEmpty();
    await expect(branchBtns.last()).not.toBeEmpty();

    const arrow = metaBranch.locator(".branch-arrow");
    await expect(arrow).toBeVisible();
  });

  test("branch names match seeded fixture data", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/1");
    await page.locator(".pull-detail")
      .waitFor({ state: "visible", timeout: 10_000 });

    const branchBtns = page.locator(
      ".meta-branch .branch-name-btn",
    );
    await expect(branchBtns.first())
      .toHaveText("feature/caching");
    await expect(branchBtns.last()).toHaveText("main");
  });
});
