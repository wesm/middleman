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
