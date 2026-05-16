import { expect, test } from "@playwright/test";

import { startIsolatedE2EServer } from "./support/e2eServer";

test.describe("CI dropdown", () => {
  test("expanded pending CI checks trigger a detail sync refresh", async ({ page }) => {
    const server = await startIsolatedE2EServer();
    try {
      await page.addInitScript(() => {
        const realSetInterval = window.setInterval;
        window.setInterval = ((handler: TimerHandler, timeout?: number, ...args: unknown[]) =>
          realSetInterval(handler, timeout === 15_000 ? 100 : timeout, ...args)) as typeof window.setInterval;
      });

      const seedResponse = await page.request.post(
        `${server.info.base_url}/__e2e/pr-ci-state/pending`,
      );
      expect(seedResponse.ok()).toBe(true);
      await expect(seedResponse.json()).resolves.toEqual({ status: "pending" });

      const backgroundSync = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return response.request().method() === "POST" &&
          url.pathname === "/api/v1/pulls/github/acme/widgets/1/sync/async";
      });

      await page.goto(`${server.info.base_url}/pulls/github/acme/widgets/1`);

      const pendingChip = page
        .locator(".pull-detail")
        .getByRole("button", { name: /CI:\s*pending \(1\)/i });
      await expect(pendingChip).toBeVisible();
      await backgroundSync;

      const successResponse = await page.request.post(
        `${server.info.base_url}/__e2e/pr-ci-state/success`,
      );
      expect(successResponse.ok()).toBe(true);
      await expect(successResponse.json()).resolves.toEqual({ status: "success" });

      const syncRequest = page.waitForRequest((request) => {
        const url = new URL(request.url());
        return request.method() === "POST" &&
          url.pathname === "/api/v1/pulls/github/acme/widgets/1/ci-refresh";
      });
      await pendingChip.click();

      await syncRequest;
      await expect(
        page
          .locator(".pull-detail")
          .getByRole("button", { name: /CI:\s*success \(4\)/i }),
      ).toBeVisible();

      const detailResponse = await page.request.get(
        `${server.info.base_url}/api/v1/pulls/github/acme/widgets/1`,
      );
      expect(detailResponse.ok()).toBe(true);
      const storedDetail = await detailResponse.json() as {
        merge_request: { CIStatus: string; CIChecksJSON: string };
      };
      expect(storedDetail.merge_request.CIStatus).toBe("success");
    } finally {
      await server.stop();
    }
  });

  test("detail chips use the shared centered chip layout", async ({ page }) => {
    await page.goto("/pulls/github/acme/widgets/1");

    const chips = page.locator(".pull-detail .chips-row .chip");
    await expect(chips.first()).toBeVisible();

    const chipLayouts = await chips.evaluateAll((nodes) => nodes.map((node) => {
        const styles = getComputedStyle(node);
        return {
          text: node.textContent?.trim() ?? "",
          minHeight: styles.minHeight,
          lineHeight: styles.lineHeight,
          paddingTop: styles.paddingTop,
          paddingBottom: styles.paddingBottom,
        };
      }));

    expect(chipLayouts.length).toBeGreaterThan(0);

    for (const chip of chipLayouts) {
      expect(chip.minHeight, chip.text).toBe("22px");
      expect(chip.lineHeight, chip.text).not.toBe("normal");
      expect(chip.paddingTop, chip.text).toBe("0px");
      expect(chip.paddingBottom, chip.text).toBe("0px");
    }
  });

  test("expanded CI checks stay below chip without stretching sibling chips", async ({ page }) => {
    await page.goto("/pulls/github/acme/widgets/1");

    const detail = page.locator(".pull-detail");
    const chip = detail.getByRole("button", { name: /CI:\s*(success|pending)/i });
    const diffStatsChip = detail.locator(".chip--muted", {
      hasText: /^\+\d+\/-\d+$/,
    });
    const actionRow = detail.locator(".primary-actions-wrap");
    await chip.waitFor({ state: "visible", timeout: 10_000 });
    const chipStylesBefore = await chip.evaluate((node) => {
      const styles = getComputedStyle(node);
      return {
        backgroundColor: styles.backgroundColor,
        paddingRight: styles.paddingRight,
        lineHeight: styles.lineHeight,
      };
    });
    const chipBox = await chip.boundingBox();
    const diffStatsBox = await diffStatsChip.boundingBox();
    const actionRowBox = await actionRow.boundingBox();
    await chip.click();

    const checks = detail.locator(".ci-checks");
    await expect(checks).toBeVisible({ timeout: 15_000 });
    await expect(detail.locator(".ci-check")).toHaveCount(4);

    const checksBox = await checks.boundingBox();
    const expandedDiffStatsBox = await diffStatsChip.boundingBox();
    const expandedActionRowBox = await actionRow.boundingBox();

    expect(chipBox).not.toBeNull();
    expect(diffStatsBox).not.toBeNull();
    expect(actionRowBox).not.toBeNull();
    expect(checksBox).not.toBeNull();
    expect(expandedDiffStatsBox).not.toBeNull();
    expect(expandedActionRowBox).not.toBeNull();
    expect(chipStylesBefore.backgroundColor).not.toBe("rgba(0, 0, 0, 0)");
    expect(chipStylesBefore.paddingRight).not.toBe("0px");
    expect(chipStylesBefore.lineHeight).not.toBe("normal");
    const ciGap = checksBox!.y - (chipBox!.y + chipBox!.height);
    expect(ciGap).toBeGreaterThan(0);
    expect(ciGap).toBeLessThan(11);
    expect(expandedDiffStatsBox!.height).toBeLessThan(40);
    expect(expandedDiffStatsBox!.y).toBe(diffStatsBox!.y);
    expect(expandedActionRowBox!.y).toBeGreaterThan(actionRowBox!.y);

    await expect(detail.locator(".ci-name")).toHaveText([
      "build",
      "lint",
      "roborev",
      "test",
    ]);
    await expect(detail.locator(".ci-duration")).toHaveText([
      "1m 30s",
      "45s",
      "2m",
    ]);
    const roborevRow = detail.locator(".ci-check", { hasText: "roborev" });
    await expect(roborevRow).toHaveCount(1);
    expect(
      await roborevRow.evaluate((node) => node.tagName),
    ).not.toBe("A");
  });
});
