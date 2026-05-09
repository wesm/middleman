import { expect, test, type Page } from "@playwright/test";

const storageKey = "middleman-pr-timeline-filter";

async function gotoWithWebKitRetry(page: Page, url: string): Promise<void> {
  let lastError: unknown;
  for (let attempt = 0; attempt < 3; attempt += 1) {
    try {
      await page.goto(url);
      return;
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      if (!message.includes("WebKit encountered an internal error")) {
        throw error;
      }
      lastError = error;
      await page.waitForTimeout(250);
    }
  }
  throw lastError;
}

async function openPRTimeline(page: Page): Promise<void> {
  await gotoWithWebKitRetry(page, "/pulls/detail?provider=github&platform_host=github.com&repo_path=acme%2Fwidgets&number=1");
  await page.locator(".pull-detail")
    .waitFor({ state: "visible", timeout: 10_000 });
  await expect(page.getByText("feat: add cache store")).toBeVisible();
  await expect(page.getByText("Cache entries now expire")).toBeVisible();
  await expect(page.getByText("Widget rendering broken on Safari"))
    .toBeVisible();
}

async function openTimelineFilters(page: Page): Promise<void> {
  await page.locator('button[title="Filter PR activity"]').click();
  await expect(page.locator(".filter-dropdown")).toBeVisible();
}

test.describe("PR timeline filters", () => {
  test.beforeEach(async ({ page }) => {
    await gotoWithWebKitRetry(page, "/");
    await page.evaluate((key) => {
      localStorage.removeItem(key);
    }, storageKey);
  });

  test("renders seeded commit and system timeline events", async ({ page }) => {
    await openPRTimeline(page);

    await expect(page.getByText("Force-pushed")).toBeVisible();
    await expect(page.getByText("abc4444 -> def5555")).toBeVisible();
    await expect(page.getByText("Referenced")).toBeVisible();
    await expect(page.getByText("Widget rendering broken on Safari"))
      .toBeVisible();
    await expect(page.getByText("Title changed")).toBeVisible();
    await expect(page.getByText(
      '"Add widget cache" -> "Add widget caching layer"',
    )).toBeVisible();
    await expect(page.getByText("Base changed")).toBeVisible();
    await expect(page.getByText("develop -> main")).toBeVisible();
  });

  test("keeps commit rows while hiding and restoring system event buckets", async ({ page }) => {
    await openPRTimeline(page);
    await openTimelineFilters(page);

    await page.getByRole("button", { name: "Commit details" }).click();
    await expect(page.getByText("feat: add cache store")).toBeVisible();
    await expect(page.getByText("Cache entries now expire")).not.toBeVisible();
    await page.getByRole("button", { name: "Commit details" }).click();
    await expect(page.getByText("feat: add cache store")).toBeVisible();
    await expect(page.getByText("Cache entries now expire")).toBeVisible();

    await page.getByRole("button", { name: "Events" }).click();
    await expect(page.getByText("Widget rendering broken on Safari"))
      .not.toBeVisible();
    await expect(page.getByText(
      '"Add widget cache" -> "Add widget caching layer"',
    )).not.toBeVisible();
    await expect(page.getByText("develop -> main")).not.toBeVisible();
    await page.getByRole("button", { name: "Events" }).click();
    await expect(page.getByText("Widget rendering broken on Safari"))
      .toBeVisible();

    await page.getByRole("button", { name: "Force pushes" }).click();
    await expect(page.getByText("abc4444 -> def5555")).not.toBeVisible();
    await page.getByRole("button", { name: "Force pushes" }).click();
    await expect(page.getByText("abc4444 -> def5555")).toBeVisible();
  });

  test("persists timeline filter preferences in localStorage", async ({ page }) => {
    await openPRTimeline(page);
    await openTimelineFilters(page);

    await page.getByRole("button", { name: "Events" }).click();
    await expect(page.getByText("Widget rendering broken on Safari"))
      .not.toBeVisible();
    await expect(page.locator('button[title="Filter PR activity"]'))
      .toContainText("1");

    await expect.poll(async () =>
      await page.evaluate((key) => localStorage.getItem(key), storageKey),
    ).toContain('"showEvents":false');

    await page.reload();
    await page.locator(".pull-detail")
      .waitFor({ state: "visible", timeout: 10_000 });
    await expect(page.getByText("Widget rendering broken on Safari"))
      .not.toBeVisible();
    await expect(page.locator('button[title="Filter PR activity"]'))
      .toContainText("1");
  });

  test("keeps commit rows when other event buckets are hidden", async ({ page }) => {
    await openPRTimeline(page);
    await openTimelineFilters(page);

    await page.getByRole("button", { name: "Messages" }).click();
    await page.getByRole("button", { name: "Commit details" }).click();
    await page.getByRole("button", { name: "Events" }).click();
    await page.getByRole("button", { name: "Force pushes" }).click();

    await expect(page.getByText("feat: add cache store")).toBeVisible();
    await expect(page.getByText("Cache entries now expire")).not.toBeVisible();
    await expect(page.getByText("No activity matches the current filters"))
      .not.toBeVisible();
  });
});
