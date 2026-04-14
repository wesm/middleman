import { expect, test } from "@playwright/test";

test.describe("CI dropdown", () => {
  test("expanded CI checks remain visible", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/1");

    const chip = page.getByRole("button", { name: /CI:\s*success/i });
    await chip.waitFor({ state: "visible", timeout: 10_000 });
    await chip.click();

    const checks = page.locator(".ci-checks");
    await expect(checks).toBeVisible();
    await expect(page.locator(".ci-check")).toHaveCount(3);

    const box = await checks.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.height).toBeGreaterThan(30);

    await expect(page.locator(".ci-check").first()).toContainText("build");
    await expect(page.locator(".ci-check").nth(1)).toContainText("test");
    await expect(page.locator(".ci-check").nth(2)).toContainText("lint");
  });
});
