import { expect, test } from "@playwright/test";

test.describe("detail action buttons", () => {
  test("pull request actions share one button frame", async ({ page }) => {
    await page.goto("/pulls");
    await page.locator(".pull-item").first()
      .waitFor({ state: "visible", timeout: 10_000 });
    await page.locator(".pull-item").filter({ hasText: "Add widget caching layer" }).first().click();
    await expect(page.locator(".pull-detail")).toBeVisible();

    await expect(page.locator(".btn--approve")).toBeVisible();
    await expect(page.locator(".btn--merge")).toBeVisible();
    await expect(page.locator(".btn--close")).toBeVisible();

    const metrics = await page.locator(
      ".btn--approve, .btn--merge, .btn--close",
    ).evaluateAll((buttons) => buttons.map((button) => {
      const rect = button.getBoundingClientRect();
      const styles = window.getComputedStyle(button);
      return {
        label: button.textContent?.trim() ?? "",
        height: Math.round(rect.height),
        radius: styles.borderRadius,
        fontSize: styles.fontSize,
        fontWeight: styles.fontWeight,
      };
    }));

    expect(metrics.length).toBeGreaterThan(0);
    const first = metrics[0]!;

    for (const metric of metrics) {
      expect(metric.height, `${metric.label} height`).toBe(first.height);
      expect(metric.radius, `${metric.label} radius`).toBe(first.radius);
      expect(metric.fontSize, `${metric.label} font size`).toBe(first.fontSize);
      expect(metric.fontWeight, `${metric.label} font weight`).toBe(first.fontWeight);
    }
  });
});
