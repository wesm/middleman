import { expect, test, type Page } from "@playwright/test";

// Seeded issues (5 total):
//   acme/widgets#10: open, eve, "Widget rendering broken on Safari"
//   acme/widgets#11: open, alice, "Add dark mode support"
//   acme/widgets#12: closed, bob, "Crash on empty input"
//   acme/widgets#13: open, dependabot[bot], "Security advisory: prototype pollution"
//   acme/tools#5: open, dave, "Support config file loading"

async function waitForIssueList(page: Page): Promise<void> {
  await page.locator(".issue-item").first()
    .waitFor({ state: "visible", timeout: 10_000 });
}

test.describe("issue list view", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/issues");
    await waitForIssueList(page);
  });

  test("renders open issues by default", async ({ page }) => {
    const countBadge = page.locator(".filter-bar .list-count-chip");
    await expect(countBadge).toHaveText(/^4 issues$/);
  });

  test("sidebar issue pills use the shared chip component", async ({ page }) => {
    await expect(page.locator(".count-badge")).toHaveCount(0);
    await expect(page.locator(".filter-bar .list-count-chip")).toHaveText(/^4 issues$/);

    const firstItem = page.locator(".issue-item").first();
    await expect(firstItem.locator(".repo-badge")).toHaveCount(0);
    await expect(firstItem.locator(".badge")).toHaveCount(0);
    await expect(firstItem.locator(".chip").first()).toBeVisible();
  });

  test("closed state shows closed issues", async ({ page }) => {
    await page.locator(".state-btn", { hasText: "Closed" }).click();

    const countBadge = page.locator(".filter-bar .list-count-chip");
    await expect(countBadge).toHaveText(/^1 issues?$/, { timeout: 5_000 });
  });

  test("search filters by title", async ({ page }) => {
    const input = page.locator(".search-input");
    await input.fill("Safari");

    // Wait for the filtered result to appear (replaces fixed sleep).
    await expect(page.locator(".filter-bar .list-count-chip"))
      .toHaveText(/^1 issues?$/, { timeout: 5_000 });

    const items = page.locator(".issue-item");
    const count = await items.count();
    expect(count).toBe(1);

    for (let i = 0; i < count; i++) {
      const title = await items.nth(i).locator(".title").textContent();
      expect(title).toContain("Safari");
    }
  });

  test("issue detail scrolls internally and centers horizontally", async ({ page }) => {
    // Open the Safari issue specifically. Matches widgets#10 on the
    // seeded fixture (max-width 800px centered layout).
    await page.locator(".issue-item")
      .filter({ hasText: "Safari" })
      .first()
      .click();

    // IssueListView renders IssueDetail into .main-area, where
    // .issue-detail is the designated internal scroll container.
    const issueDetail = page.locator(".issue-detail");
    await expect(issueDetail).toBeVisible();

    // Inject a tall filler so overflow is guaranteed even with the
    // short seeded body. flex-shrink: 0 is required because
    // .issue-detail is a flex column; without it, the child would be
    // shrunk to fit.
    await issueDetail.evaluate((el) => {
      const filler = document.createElement("div");
      filler.style.height = "3000px";
      filler.style.flexShrink = "0";
      filler.style.background = "transparent";
      filler.setAttribute("data-test-filler", "issue-scroll");
      el.appendChild(filler);
    });

    // .issue-detail owns vertical scroll (overflow-y: auto in the
    // component style).
    const overflowY = await issueDetail.evaluate(
      (el) => getComputedStyle(el).overflowY,
    );
    expect(["auto", "scroll"]).toContain(overflowY);

    const before = await issueDetail.evaluate((el) => ({
      scrollHeight: el.scrollHeight,
      clientHeight: el.clientHeight,
      scrollTop: el.scrollTop,
    }));
    expect(before.scrollHeight).toBeGreaterThan(before.clientHeight);
    expect(before.scrollTop).toBe(0);

    await issueDetail.evaluate((el) => {
      el.scrollTop = el.scrollHeight;
    });

    const finalScroll = await issueDetail.evaluate((el) => el.scrollTop);
    expect(finalScroll).toBeGreaterThan(0);

    // Centering check: .issue-detail has max-width 800px and
    // margin-inline: auto. Compare its horizontal center against the
    // center of its parent .main-area container (the PR list view
    // has a sidebar, so the viewport center is not relevant).
    const detailArea = page.locator(".main-area");
    const areaBox = await detailArea.boundingBox();
    const detailBox = await issueDetail.boundingBox();
    expect(areaBox).not.toBeNull();
    expect(detailBox).not.toBeNull();
    if (areaBox !== null && detailBox !== null) {
      const areaCenter = areaBox.x + areaBox.width / 2;
      const detailCenter = detailBox.x + detailBox.width / 2;
      // Allow small slack for sub-pixel layout differences.
      expect(Math.abs(detailCenter - areaCenter)).toBeLessThan(2);
    }
  });
});
