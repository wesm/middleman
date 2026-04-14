import { expect, test, type Page } from "@playwright/test";

async function waitForPRList(page: Page): Promise<void> {
  await page.locator(".pull-item").first()
    .waitFor({ state: "visible", timeout: 10_000 });
}

async function waitForIssueList(page: Page): Promise<void> {
  await page.locator(".issue-item").first()
    .waitFor({ state: "visible", timeout: 10_000 });
}

test.describe("collapsible sidebar", () => {
  test("collapse and expand via strip on pulls", async ({ page }) => {
    await page.goto("/pulls");
    await waitForPRList(page);

    const sidebar = page.locator(".sidebar");
    await expect(sidebar).toBeVisible();
    await expect(sidebar).not.toHaveClass(/sidebar--collapsed/);

    // Click the collapse button inside the sidebar.
    await sidebar.locator(".sidebar-toggle").click();
    await expect(sidebar).toHaveClass(/sidebar--collapsed/);

    // The expand button should now appear in the collapsed strip.
    const expandBtn = sidebar.locator(".expand-btn");
    await expect(expandBtn).toBeVisible();

    // Click the expand button to restore the sidebar.
    await expandBtn.click();
    await expect(sidebar).not.toHaveClass(/sidebar--collapsed/);
  });

  test("collapse and expand via strip on issues", async ({ page }) => {
    await page.goto("/issues");
    await waitForIssueList(page);

    const sidebar = page.locator(".sidebar");
    await expect(sidebar).toBeVisible();
    await expect(sidebar).not.toHaveClass(/sidebar--collapsed/);

    await sidebar.locator(".sidebar-toggle").click();
    await expect(sidebar).toHaveClass(/sidebar--collapsed/);

    const expandBtn = sidebar.locator(".expand-btn");
    await expect(expandBtn).toBeVisible();

    await expandBtn.click();
    await expect(sidebar).not.toHaveClass(/sidebar--collapsed/);
  });

  test("header expand on non-list route after collapsing", async ({ page }) => {
    await page.goto("/pulls");
    await waitForPRList(page);

    // Collapse sidebar on list view.
    const sidebar = page.locator(".sidebar");
    await sidebar.locator(".sidebar-toggle").click();
    await expect(sidebar).toHaveClass(/sidebar--collapsed/);

    // Navigate to board view (no sidebar strip).
    await page.goto("/pulls/board");
    await page.waitForTimeout(300);

    // Header expand button should be visible.
    const headerToggle = page.locator(".app-header .sidebar-toggle");
    await expect(headerToggle).toBeVisible();

    // Click it to expand.
    await headerToggle.click();

    // Navigate back to list view and verify sidebar is expanded.
    await page.goto("/pulls");
    await waitForPRList(page);
    const restoredSidebar = page.locator(".sidebar");
    await expect(restoredSidebar).not.toHaveClass(/sidebar--collapsed/);
  });

  test("keyboard shortcut toggles sidebar", async ({ page }) => {
    await page.goto("/pulls");
    await waitForPRList(page);

    const sidebar = page.locator(".sidebar");
    await expect(sidebar).not.toHaveClass(/sidebar--collapsed/);

    // Press Cmd+[ (macOS) or Ctrl+[ to collapse.
    const modifier = process.platform === "darwin" ? "Meta" : "Control";
    await page.keyboard.press(`${modifier}+[`);
    await expect(sidebar).toHaveClass(/sidebar--collapsed/);

    // Press again to expand.
    await page.keyboard.press(`${modifier}+[`);
    await expect(sidebar).not.toHaveClass(/sidebar--collapsed/);
  });
});
