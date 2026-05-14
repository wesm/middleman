import { devices, expect, test, type Page } from "@playwright/test";

import { mockApi } from "./support/mockApi";

async function mockMobileRepoSettings(page: Page): Promise<string[]> {
  const activityRepos: string[] = [];

  await mockApi(page);
  await page.route("**/api/v1/settings", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        repos: [
          {
            provider: "github",
            platform_host: "github.com",
            owner: "acme",
            name: "widgets",
            repo_path: "acme/widgets",
            is_glob: false,
            matched_repo_count: 1,
          },
          {
            provider: "github",
            platform_host: "ghe.example.com",
            owner: "acme",
            name: "widgets",
            repo_path: "acme/widgets",
            is_glob: false,
            matched_repo_count: 1,
          },
          {
            provider: "github",
            platform_host: "github.com",
            owner: "acme",
            name: "*",
            repo_path: "acme/*",
            is_glob: true,
            matched_repo_count: 4,
          },
        ],
        activity: {
          view_mode: "threaded",
          time_range: "30d",
          hide_closed: false,
          hide_bots: false,
        },
        terminal: { font_family: "", renderer: "xterm" },
        agents: [],
      }),
    });
  });
  await page.route("**/api/v1/activity**", async (route) => {
    const url = new URL(route.request().url());
    const repo = url.searchParams.get("repo");
    if (repo) activityRepos.push(repo);
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ capped: false, items: [] }),
    });
  });

  return activityRepos;
}

test.use({ ...devices["iPhone 13"] });

test.describe("mobile activity repository selector", () => {
  test("uses host-qualified concrete repos and excludes glob rows", async ({ page }) => {
    const activityRepos = await mockMobileRepoSettings(page);

    await page.goto("/m?range=30d&view=threaded");
    const repoSelect = page.getByLabel("Repository");
    await expect(repoSelect).toBeVisible();

    await expect(repoSelect.locator("option")).toHaveText([
      "All repos",
      "github.com/acme/widgets",
      "ghe.example.com/acme/widgets",
    ]);
    await expect(repoSelect.locator("option", { hasText: "acme/*" }))
      .toHaveCount(0);

    await repoSelect.selectOption("ghe.example.com/acme/widgets");
    await expect(repoSelect).toHaveValue("ghe.example.com/acme/widgets");
    await expect.poll(() => activityRepos)
      .toContain("ghe.example.com/acme/widgets");
  });

  test("groups and labels activity from nested repo identity", async ({ page }) => {
    await mockMobileRepoSettings(page);
    await page.route("**/api/v1/activity**", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          capped: false,
          items: [
            {
              id: "a1",
              cursor: "a1",
              activity_type: "comment",
              author: "marius",
              body_preview: "Looks good",
              created_at: "2026-03-30T14:00:00Z",
              item_number: 42,
              item_state: "open",
              item_title: "Add browser regression coverage",
              item_type: "pr",
              item_url: "https://github.com/acme/widgets/pull/42",
              repo: {
                provider: "github",
                platform_host: "github.com",
                owner: "acme",
                name: "widgets",
                repo_path: "acme/widgets",
                capabilities: {},
              },
            },
            {
              id: "b1",
              cursor: "b1",
              activity_type: "review",
              author: "luisa",
              body_preview: "Requested tweaks",
              created_at: "2026-03-30T13:00:00Z",
              item_number: 42,
              item_state: "open",
              item_title: "Add browser regression coverage",
              item_type: "pr",
              item_url: "https://github.com/beta/gadgets/pull/42",
              repo: {
                provider: "github",
                platform_host: "github.com",
                owner: "beta",
                name: "gadgets",
                repo_path: "beta/gadgets",
                capabilities: {},
              },
            },
          ],
        }),
      });
    });

    await page.goto("/m?range=30d&view=threaded");

    await expect(page.locator(".mobile-activity-card")).toHaveCount(2);
    await expect(page.locator(".mobile-activity-card__meta", { hasText: "acme/widgets" }))
      .toBeVisible();
    await expect(page.locator(".mobile-activity-card__meta", { hasText: "beta/gadgets" }))
      .toBeVisible();
    await expect(page.getByText("undefined/undefined")).toHaveCount(0);
  });
});
