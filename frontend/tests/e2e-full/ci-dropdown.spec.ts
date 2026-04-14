import { expect, test } from "@playwright/test";

test.describe("CI dropdown", () => {
  test("expanded CI checks stay below chip without stretching sibling chips", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/1");

    const detail = page.locator(".pull-detail");
    const chip = detail.getByRole("button", { name: /CI:\s*(success|pending)/i });
    await chip.waitFor({ state: "visible", timeout: 10_000 });
    const chipBox = await chip.boundingBox();
    await chip.click();

    const checks = detail.locator(".ci-checks");
    await expect(checks).toBeVisible();
    await expect(detail.locator(".ci-check")).toHaveCount(4);

    const checksBox = await checks.boundingBox();
    const additionsChipBox = await detail.locator(".chip--muted").boundingBox();

    expect(chipBox).not.toBeNull();
    expect(checksBox).not.toBeNull();
    expect(additionsChipBox).not.toBeNull();
    expect(checksBox!.y).toBeGreaterThan(chipBox!.y + chipBox!.height);
    expect(additionsChipBox!.height).toBeLessThan(40);

    await expect(detail.locator(".ci-check").first()).toContainText("build");
    await expect(detail.locator(".ci-check").nth(1)).toContainText("test");
    await expect(detail.locator(".ci-check").nth(2)).toContainText("lint");
    const roborevRow = detail.locator(".ci-check", { hasText: "roborev" });
    await expect(roborevRow).toHaveCount(1);
    expect(
      await roborevRow.evaluate((node) => node.tagName),
    ).not.toBe("A");
  });
});
