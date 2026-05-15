import { expect, test } from "@playwright/test";

type PRDetailResponseForTest = {
  merge_request: {
    CIStatus: string;
    CIChecksJSON: string;
    [key: string]: unknown;
  };
  [key: string]: unknown;
};

test.describe("CI dropdown", () => {
  test("expanded pending CI checks trigger a detail sync refresh", async ({ page }) => {
    let pendingDetail: PRDetailResponseForTest | null = null;
    const pendingChecks = [
      {
        name: "build",
        status: "in_progress",
        conclusion: "",
        url: "https://ci.example.com/build",
        app: "GitHub Actions",
      },
    ];
    await page.route("**/api/v1/pulls/github/acme/widgets/1", async (route) => {
      const response = await route.fetch();
      pendingDetail = await response.json() as PRDetailResponseForTest;
      pendingDetail!.merge_request.CIStatus = "pending";
      pendingDetail!.merge_request.CIChecksJSON = JSON.stringify(pendingChecks);
      await route.fulfill({ response, json: pendingDetail });
    });

    const syncRequests: string[] = [];
    await page.route("**/api/v1/pulls/github/acme/widgets/1/sync", async (route) => {
      syncRequests.push(route.request().method());
      const syncedDetail = {
        ...pendingDetail,
        merge_request: {
          ...pendingDetail!.merge_request,
          CIStatus: "success",
          CIChecksJSON: JSON.stringify([
            {
              ...pendingChecks[0],
              status: "completed",
              conclusion: "success",
            },
          ]),
        },
      };
      await route.fulfill({ status: 200, json: syncedDetail });
    });

    await page.goto("/pulls/github/acme/widgets/1");

    await page
      .locator(".pull-detail")
      .getByRole("button", { name: /CI:\s*pending \(1\)/i })
      .click();

    await expect.poll(() => syncRequests.length, {
      timeout: 17_000,
    }).toBeGreaterThan(0);
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
