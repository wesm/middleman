import { expect, test } from "@playwright/test";

test.describe("focus mode", () => {
  test("PR focus route renders detail without shell chrome", async ({ page }) => {
    await page.goto("/focus/pulls/github/acme/widgets/1");
    await page.locator(".focus-layout").waitFor({ state: "visible", timeout: 10_000 });

    await expect(page.locator(".pull-detail")).toBeVisible();
    await expect(page.locator(".app-header")).not.toBeAttached();
    await expect(page.locator(".sidebar")).not.toBeAttached();
    await expect(page.locator(".status-bar")).not.toBeAttached();
  });

  test("issue focus route renders detail without shell chrome", async ({ page }) => {
    await page.goto("/focus/issues/github/acme/widgets/10");
    await page.locator(".focus-layout").waitFor({ state: "visible", timeout: 10_000 });

    await expect(page.locator(".issue-detail")).toBeVisible();
    await expect(page.locator(".app-header")).not.toBeAttached();
    await expect(page.locator(".sidebar")).not.toBeAttached();
    await expect(page.locator(".status-bar")).not.toBeAttached();
  });

  test("narrow PR focus route shows actions only inside the actions menu", async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 720 });
    await page.goto("/focus/pulls/github/acme/widgets/1");
    await page.locator(".focus-layout .pull-detail").waitFor({ state: "visible", timeout: 10_000 });

    await expect(page.locator(".actions-row--primary")).toBeHidden();
    await expect(page.locator(".label-editor-anchor--inline")).toBeHidden();
    await expect(page.locator(".actions-row--workspace")).toBeHidden();
    await expect(page.locator(".actions-menu-trigger")).toBeVisible();

    await page.locator(".actions-menu-trigger").click();

    await expect(page.locator(".actions-row--primary .btn--approve")).toBeHidden();
    await expect(page.locator(".actions-menu-popover .btn--approve")).toBeVisible();
    await expect(page.locator(".actions-menu-popover .btn--merge")).toBeVisible();
    await expect(page.locator(".actions-menu-popover .btn--close")).toBeVisible();
    await expect(page.locator(".actions-menu-popover").getByRole("button", { name: "Labels" })).toBeVisible();
    await expect(page.locator(".actions-menu-popover .btn--workspace")).toBeVisible();
  });

  test("browser back/forward works between focus routes", async ({ page }) => {
    await page.goto("/focus/pulls/github/acme/widgets/1");
    await page.locator(".pull-detail").waitFor({ state: "visible", timeout: 10_000 });

    // Navigate forward to an issue focus route.
    await page.goto("/focus/issues/github/acme/widgets/10");
    await page.locator(".issue-detail").waitFor({ state: "visible", timeout: 10_000 });
    await expect(page).toHaveURL(
      /\/focus\/issues\/github\/acme\/widgets\/10$/,
    );

    // Go back to the PR focus route.
    await page.goBack();
    await page.locator(".pull-detail").waitFor({ state: "visible", timeout: 10_000 });
    await expect(page).toHaveURL(
      /\/focus\/pulls\/github\/acme\/widgets\/1$/,
    );

    // Go forward to the issue focus route.
    await page.goForward();
    await page.locator(".issue-detail").waitFor({ state: "visible", timeout: 10_000 });
    await expect(page).toHaveURL(
      /\/focus\/issues\/github\/acme\/widgets\/10$/,
    );
  });
});
