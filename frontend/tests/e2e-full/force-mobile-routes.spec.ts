import { expect, test } from "@playwright/test";

test("force-mobile flag sends desktop viewport issue route to phone route", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.addInitScript(() => {
    (window as unknown as { __MIDDLEMAN_FORCE_MOBILE_ROUTES__?: boolean }).__MIDDLEMAN_FORCE_MOBILE_ROUTES__ = true;
  });

  await page.goto("/issues");

  await expect(page).toHaveURL(/\/m\/issues(?:\?|$)/);
  await expect(page.locator(".mobile-shell")).toBeVisible();
  await expect(page.locator(".mobile-tab--active")).toHaveText("Issues");
  await expect(page.locator(".app-header")).toHaveCount(0);
});
