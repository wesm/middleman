import { expect, test } from "@playwright/test";

test.describe("container-aware layout", () => {
  test("narrow viewport shows dropdown and collapses sidebar", async ({ page }) => {
    await page.setViewportSize({ width: 400, height: 600 });
    await page.goto("/pulls?desktop=1");
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

  test("mobile viewport wraps header controls without horizontal overflow", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 700 });
    await page.goto("/pulls?desktop=1");
    const header = page.locator(".app-header");
    await header.waitFor({ state: "visible", timeout: 10_000 });

    await expect(page.locator(".nav-select")).toBeVisible();
    await expect(page.getByRole("button", { name: "Sync" })).toBeVisible();

    const metrics = await page.evaluate(() => {
      const headerRect = document.querySelector(".app-header")
        ?.getBoundingClientRect();
      return {
        headerHeight: headerRect?.height ?? 0,
        viewportWidth: window.innerWidth,
        documentWidth: document.documentElement.scrollWidth,
        bodyWidth: document.body.scrollWidth,
      };
    });

    expect(metrics.headerHeight).toBeGreaterThanOrEqual(76);
    expect(Math.max(metrics.documentWidth, metrics.bodyWidth)).toBeLessThanOrEqual(
      metrics.viewportWidth,
    );
  });

  test("expanded mobile sidebar fits within the viewport", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 700 });
    await page.goto("/pulls?desktop=1");
    await page.locator(".app-header")
      .waitFor({ state: "visible", timeout: 10_000 });

    await page.getByLabel("Expand sidebar").click();
    await expect(page.locator(".sidebar").first()).not.toHaveClass(
      /sidebar--collapsed/,
    );

    const metrics = await page.evaluate(() => {
      const sidebarRect = document.querySelector(".sidebar")
        ?.getBoundingClientRect();
      return {
        viewportWidth: window.innerWidth,
        sidebarRight: sidebarRect?.right ?? 0,
        sidebarWidth: sidebarRect?.width ?? 0,
        documentWidth: document.documentElement.scrollWidth,
        bodyWidth: document.body.scrollWidth,
      };
    });

    expect(metrics.sidebarWidth).toBeLessThanOrEqual(metrics.viewportWidth);
    expect(metrics.sidebarRight).toBeLessThanOrEqual(metrics.viewportWidth);
    expect(Math.max(metrics.documentWidth, metrics.bodyWidth)).toBeLessThanOrEqual(
      metrics.viewportWidth,
    );
  });

  test("wide viewport shows tab group and hides dropdown", async ({ page }) => {
    // Start narrow, then go wide to verify transition.
    await page.setViewportSize({ width: 400, height: 600 });
    await page.goto("/pulls?desktop=1");
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
