import { expect, test } from "@playwright/test";

test.describe("detail action buttons", () => {
  test("pull request actions use shared ActionButton component", async ({ page }) => {
    await page.goto("/pulls");
    await page.locator(".pull-item").first()
      .waitFor({ state: "visible", timeout: 10_000 });
    await page.locator(".pull-item").filter({ hasText: "Add widget caching layer" }).first().click();
    await expect(page.locator(".pull-detail")).toBeVisible();

    const approve = page.locator(".btn--approve");
    const merge = page.locator(".btn--merge");
    const close = page.locator(".btn--close");

    await expect(approve).toBeVisible();
    await expect(merge).toBeVisible();
    await expect(close).toBeVisible();

    // All action buttons use the shared action-button base class
    for (const btn of [approve, merge, close]) {
      const classes = await btn.getAttribute("class");
      expect(classes).toContain("action-button");
    }
  });
});
