import { expect, test, type Page } from "@playwright/test";

// Seeded issues (6 total):
//   acme/widgets#10: open, eve, "Widget rendering broken on Safari"
//   acme/widgets#11: open, alice, "Add dark mode support"
//   acme/widgets#12: closed, bob, "Crash on empty input"
//   acme/widgets#13: open, dependabot[bot], "Security advisory: prototype pollution"
//   acme/tools#5: open, dave, "Support config file loading"
//   group/project#11: open, ada, "GitLab read-only issue"

async function waitForIssueList(page: Page): Promise<void> {
  await page
    .locator(".issue-item")
    .first()
    .waitFor({ state: "visible", timeout: 10_000 });
}

async function selectIssueState(page: Page, label: string): Promise<void> {
  const stateButton = page.locator(".state-btn", { hasText: label });
  if (await stateButton.isVisible()) {
    await stateButton.click();
    return;
  }

  await page.getByRole("button", { name: "Filters" }).click();
  await page.locator(".filter-dropdown .filter-item", { hasText: label })
    .first()
    .click();
}

async function selectIssueGrouping(page: Page, label: string): Promise<void> {
  const groupButton = page.locator(".group-btn", { hasText: label });
  if (await groupButton.isVisible()) {
    await groupButton.click();
    return;
  }

  await page.getByRole("button", { name: "Filters" }).click();
  await page.locator(".filter-dropdown .filter-item", { hasText: label })
    .last()
    .click();
}

test.describe("issue list view", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/issues");
    await waitForIssueList(page);
  });

  test("renders open issues by default", async ({ page }) => {
    const countBadge = page.locator(".filter-bar .list-count-chip");
    await expect(countBadge).toHaveText(/^5 issues$/);
  });

  test("sidebar issue pills use the shared chip component", async ({
    page,
  }) => {
    await expect(page.locator(".filter-bar .list-count-chip")).toHaveText(
      /^5 issues$/,
    );

    await selectIssueGrouping(page, "All");
    const firstItem = page.locator(".issue-item").first();
    const repoChip = firstItem.locator(".repo-chip");
    await expect(repoChip).toBeVisible();
    await expect(repoChip.locator(".chip__label")).toHaveText("acme/widgets");
    await expect(repoChip).toHaveAttribute("title", "acme/widgets");
    await expect(repoChip).toHaveCSS("justify-content", "flex-start");
    await expect(firstItem.locator(".state-chip")).toBeVisible();
  });

  test("closed state shows closed issues", async ({ page }) => {
    await selectIssueState(page, "Closed");

    const countBadge = page.locator(".filter-bar .list-count-chip");
    await expect(countBadge).toHaveText(/^1 issues?$/, { timeout: 5_000 });
  });

  test("search filters by title", async ({ page }) => {
    const input = page.locator(".search-input");
    await input.fill("Safari");

    // Wait for the filtered result to appear (replaces fixed sleep).
    await expect(page.locator(".filter-bar .list-count-chip")).toHaveText(
      /^1 issues?$/,
      { timeout: 5_000 },
    );

    const items = page.locator(".issue-item");
    const count = await items.count();
    expect(count).toBe(1);

    for (let i = 0; i < count; i++) {
      const title = await items.nth(i).locator(".title").textContent();
      expect(title).toContain("Safari");
    }
  });

  test("issue detail state chip preserves shared chip layout", async ({
    page,
  }) => {
    await page
      .locator(".issue-item")
      .filter({ hasText: "Safari" })
      .first()
      .click();

    const stateChip = page.locator(".issue-detail .issue-state-chip");
    await expect(stateChip).toBeVisible();
    await expect(stateChip).toHaveText("Open");

    const stateChipStyles = await stateChip.evaluate((node) => {
      const styles = getComputedStyle(node);
      return {
        minHeight: styles.minHeight,
        fontSize: styles.fontSize,
        backgroundColor: styles.backgroundColor,
      };
    });

    expect(stateChipStyles.minHeight).toBe("18px");
    expect(stateChipStyles.fontSize).toBe("10px");
    expect(stateChipStyles.backgroundColor).not.toBe("rgba(0, 0, 0, 0)");
  });

  test("issue detail keeps the scrollbar on the pane edge", async ({
    page,
  }) => {
    // Open the Safari issue specifically. Matches widgets#10 on the
    // seeded fixture (max-width 800px centered layout).
    await page
      .locator(".issue-item")
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

    // The scroll container should span the detail pane so the native
    // scrollbar is flush with the pane edge, not the centered content
    // column. The header remains in the capped content column.
    const detailArea = page.locator(".main-area");
    const contentHeader = page.locator(".issue-detail .detail-header");
    const areaBox = await detailArea.boundingBox();
    const detailBox = await issueDetail.boundingBox();
    const headerBox = await contentHeader.boundingBox();
    expect(areaBox).not.toBeNull();
    expect(detailBox).not.toBeNull();
    expect(headerBox).not.toBeNull();
    if (areaBox !== null && detailBox !== null && headerBox !== null) {
      const scrollportWidth = await issueDetail.evaluate(
        (el) => el.clientWidth,
      );
      const scrollportCenter = detailBox.x + scrollportWidth / 2;
      const headerCenter = headerBox.x + headerBox.width / 2;
      // Allow small slack for sub-pixel layout differences.
      expect(
        Math.abs(detailBox.x + detailBox.width - (areaBox.x + areaBox.width)),
      ).toBeLessThan(2);
      expect(Math.abs(headerCenter - scrollportCenter)).toBeLessThan(2);
      expect(headerBox.width).toBeLessThanOrEqual(800);
    }
  });
});
