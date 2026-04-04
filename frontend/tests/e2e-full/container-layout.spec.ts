import { expect, test } from "@playwright/test";

test.describe("container-aware layout", () => {
  test("narrow viewport shows dropdown and collapses sidebar", async ({ page }) => {
    await page.setViewportSize({ width: 400, height: 600 });
    await page.goto("/pulls");
    // At narrow width the sidebar is auto-collapsed, so .pull-item
    // won't be visible. Wait for the app header instead.
    await page.locator(".app-header")
      .waitFor({ state: "visible", timeout: 10_000 });

    // Narrow: dropdown navigation visible, tab group hidden.
    await expect(page.locator(".nav-select")).toBeVisible();
    await expect(page.locator(".tab-group")).not.toBeAttached();

    // Sidebar should be auto-collapsed in narrow mode.
    await expect(page.locator(".sidebar")).toHaveClass(/sidebar--collapsed/);
  });

  test("wide viewport shows tab group and hides dropdown", async ({ page }) => {
    // Start narrow, then go wide to verify transition.
    await page.setViewportSize({ width: 400, height: 600 });
    await page.goto("/pulls");
    await page.locator(".app-header")
      .waitFor({ state: "visible", timeout: 10_000 });

    await expect(page.locator(".nav-select")).toBeVisible();

    // Switch to wide viewport.
    await page.setViewportSize({ width: 1280, height: 720 });

    // Wait for the resize observer debounce (100ms) to settle.
    await expect(page.locator(".tab-group")).toBeVisible({ timeout: 5_000 });
    await expect(page.locator(".nav-select")).not.toBeAttached();
  });
});
