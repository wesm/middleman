import { expect, test, type Page } from "@playwright/test";

async function waitForPRList(page: Page): Promise<void> {
  await page.locator(".pull-item").first()
    .waitFor({ state: "visible", timeout: 10_000 });
}

test.describe("embedded config", () => {
  test("hides sync button when hideSync is true", async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = { ui: { hideSync: true } };
    });
    await page.goto("/pulls");
    await waitForPRList(page);

    await expect(
      page.locator(".action-btn", { hasText: "Sync" }),
    ).not.toBeVisible();
  });

  test("hides repo selector when hideRepoSelector is true", async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = { ui: { hideRepoSelector: true } };
    });
    await page.goto("/pulls");
    await waitForPRList(page);

    await expect(page.locator(".typeahead")).not.toBeAttached();
  });

  test("hides star button when hideStar is true", async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = { ui: { hideStar: true } };
    });
    await page.goto("/pulls");
    await waitForPRList(page);

    // Open a PR detail.
    await page.locator(".pull-item").first().click();
    await page.locator(".pull-detail").waitFor({ state: "visible", timeout: 10_000 });

    await expect(
      page.locator(".pull-detail .star-btn"),
    ).not.toBeAttached();
  });

  test("hides theme toggle when theme.mode is set", async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = { theme: { mode: "dark" } };
    });
    await page.goto("/pulls");
    await waitForPRList(page);

    await expect(
      page.locator("button[title='Toggle theme']"),
    ).not.toBeAttached();
  });

  test("settings page is blocked in embedded mode", async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {};
    });
    await page.goto("/settings");

    // When embedded, /settings is not a valid route and falls
    // through to the activity page. The URL may still say /settings
    // but the activity feed should render instead.
    await page.locator(".activity-feed")
      .waitFor({ state: "visible", timeout: 10_000 });
    await expect(page.locator(".settings-page")).not.toBeAttached();
  });
});
