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

  test("draft pull request actions keep exactly the same height", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/6");
    await expect(page.locator(".pull-detail")).toBeVisible();

    const ready = page.locator(".btn--ready");
    const approve = page.locator(".btn--approve");
    const merge = page.locator(".btn--merge");
    const close = page.locator(".btn--close");

    for (const btn of [ready, approve, merge, close]) {
      await expect(btn).toBeVisible();
    }

    const metrics = await page.evaluate(() => {
      const selectors = [".btn--ready", ".btn--approve", ".btn--merge", ".btn--close"];
      return selectors.map((selector) => {
        const element = document.querySelector(selector);
        if (!(element instanceof HTMLElement)) {
          throw new Error(`missing action button: ${selector}`);
        }
        const rect = element.getBoundingClientRect();
        return {
          selector,
          height: element.offsetHeight,
          top: Math.round(rect.top),
          left: Math.round(rect.left),
          right: Math.round(rect.right),
        };
      });
    });

    expect(metrics.map((metric) => metric.height)).toEqual(
      Array(metrics.length).fill(metrics[0]?.height),
    );
    expect(new Set(metrics.map((metric) => metric.top)).size).toBe(1);
    expect(
      metrics.slice(0, -1).map((metric, index) => metrics[index + 1]!.left - metric.right),
    ).toEqual(
      Array(metrics.length - 1).fill(
        metrics[1] ? metrics[1].left - metrics[0]!.right : 0,
      ),
    );
  });
});
