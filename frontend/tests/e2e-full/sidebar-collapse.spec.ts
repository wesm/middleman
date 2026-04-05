import { expect, test, type Page } from "@playwright/test";

async function waitForPRList(page: Page): Promise<void> {
  await page.locator(".pull-item").first()
    .waitFor({ state: "visible", timeout: 10_000 });
}

test.describe("collapsible sidebar", () => {
  test("collapse and expand via buttons", async ({ page }) => {
    await page.goto("/pulls");
    await waitForPRList(page);

    const sidebar = page.locator(".sidebar");
    await expect(sidebar).toBeVisible();
    await expect(sidebar).not.toHaveClass(/sidebar--collapsed/);

    // Click the collapse button inside the sidebar.
    await sidebar.locator(".sidebar-toggle").click();
    await expect(sidebar).toHaveClass(/sidebar--collapsed/);

    // The expand button should now appear in the header.
    const headerToggle = page.locator(".app-header .sidebar-toggle");
    await expect(headerToggle).toBeVisible();

    // Click the header expand button to restore the sidebar.
    await headerToggle.click();
    await expect(sidebar).not.toHaveClass(/sidebar--collapsed/);
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
