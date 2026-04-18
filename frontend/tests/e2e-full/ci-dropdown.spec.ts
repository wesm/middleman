import { expect, test } from "@playwright/test";

test.describe("CI dropdown", () => {
  test("expanded CI checks stay below chip without stretching sibling chips", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/1");

    const detail = page.locator(".pull-detail");
    const chip = detail.getByRole("button", { name: /CI:\s*(success|pending)/i });
    const diffStatsChip = detail.locator(".chip--muted", {
      hasText: /^\+\d+\/-\d+$/,
    });
    const statusRow = detail.locator(".kanban-row");
    await chip.waitFor({ state: "visible", timeout: 10_000 });
    const chipStylesBefore = await chip.evaluate((node) => {
      const styles = getComputedStyle(node);
      return {
        backgroundColor: styles.backgroundColor,
        paddingTop: styles.paddingTop,
        paddingRight: styles.paddingRight,
      };
    });
    const chipBox = await chip.boundingBox();
    const diffStatsBox = await diffStatsChip.boundingBox();
    const statusRowBox = await statusRow.boundingBox();
    await chip.click();

    const checks = detail.locator(".ci-checks");
    await expect(checks).toBeVisible();
    await expect(detail.locator(".ci-check")).toHaveCount(4);

    const checksBox = await checks.boundingBox();
    const expandedDiffStatsBox = await diffStatsChip.boundingBox();
    const expandedStatusRowBox = await statusRow.boundingBox();

    expect(chipBox).not.toBeNull();
    expect(diffStatsBox).not.toBeNull();
    expect(statusRowBox).not.toBeNull();
    expect(checksBox).not.toBeNull();
    expect(expandedDiffStatsBox).not.toBeNull();
    expect(expandedStatusRowBox).not.toBeNull();
    expect(chipStylesBefore.backgroundColor).not.toBe("rgba(0, 0, 0, 0)");
    expect(chipStylesBefore.paddingTop).not.toBe("0px");
    expect(chipStylesBefore.paddingRight).not.toBe("0px");
    const ciGap = checksBox!.y - (chipBox!.y + chipBox!.height);
    expect(ciGap).toBeGreaterThan(0);
    expect(ciGap).toBeLessThan(11);
    expect(expandedDiffStatsBox!.height).toBeLessThan(40);
    expect(expandedDiffStatsBox!.y).toBe(diffStatsBox!.y);
    expect(expandedStatusRowBox!.y).toBeGreaterThan(statusRowBox!.y);

    await expect(detail.locator(".ci-name")).toHaveText([
      "build",
      "lint",
      "roborev",
      "test",
    ]);
    const roborevRow = detail.locator(".ci-check", { hasText: "roborev" });
    await expect(roborevRow).toHaveCount(1);
    expect(
      await roborevRow.evaluate((node) => node.tagName),
    ).not.toBe("A");
  });
});
