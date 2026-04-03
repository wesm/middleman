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
    const countBadge = page.locator(".count-badge");
    await expect(countBadge).toHaveText(/^4 issues$/);
  });

  test("closed state shows closed issues", async ({ page }) => {
    await page.locator(".state-btn", { hasText: "Closed" }).click();

    const countBadge = page.locator(".count-badge");
    await expect(countBadge).toHaveText(/^1 issues?$/, { timeout: 5_000 });
  });

  test("search filters by title", async ({ page }) => {
    const input = page.locator(".search-input");
    await input.fill("Safari");

    // Wait for the filtered result to appear (replaces fixed sleep).
    await expect(page.locator(".count-badge"))
      .toHaveText(/^1 issues?$/, { timeout: 5_000 });

    const items = page.locator(".issue-item");
    const count = await items.count();
    expect(count).toBe(1);

    for (let i = 0; i < count; i++) {
      const title = await items.nth(i).locator(".title").textContent();
      expect(title).toContain("Safari");
    }
  });
});
