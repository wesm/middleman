import { expect, request as playwrightRequest, test, type APIRequestContext } from "@playwright/test";
import { startIsolatedE2EServer, type IsolatedE2EServer } from "./support/e2eServer";

type RepoSummary = {
  Owner: string;
  Name: string;
};

let isolatedServer: IsolatedE2EServer | undefined;
let api: APIRequestContext | undefined;

test.beforeEach(async () => {
  isolatedServer = await startIsolatedE2EServer();
  api = await playwrightRequest.newContext({
    baseURL: isolatedServer.info.base_url,
  });
});

test.afterEach(async () => {
  await api?.dispose();
  await isolatedServer?.stop();
  api = undefined;
  isolatedServer = undefined;
});

test("settings shows glob match counts and refresh updates tracked repos", async ({ page }) => {
  await page.goto(`${isolatedServer!.info.base_url}/settings`);

  await page.locator(".settings-page").waitFor({ state: "visible", timeout: 10_000 });

  const row = page.locator(".repo-row", { hasText: "roborev-dev/*" });
  await expect(row).toContainText("roborev-dev/*");
  await expect(row).toContainText("(2)");
  await expect.poll(async () => {
    if (!api) {
      throw new Error("settings-globs API context not initialized");
    }
    const response = await api.get("/api/v1/repos");
    const repos = await response.json() as RepoSummary[];
    return repos
      .filter((repo) => repo.Owner === "roborev-dev")
      .map((repo) => repo.Name)
      .sort()
      .join(",");
  }).toBe("middleman,worker");

  await row.getByRole("button", { name: "Refresh" }).click();

  await expect(row).toContainText("(3)");
  await expect.poll(async () => {
    if (!api) {
      throw new Error("settings-globs API context not initialized");
    }
    const response = await api.get("/api/v1/repos");
    const repos = await response.json() as RepoSummary[];
    return repos
      .filter((repo) => repo.Owner === "roborev-dev")
      .map((repo) => repo.Name)
      .sort()
      .join(",");
  }).toBe("middleman,review-bot,worker");

  await page.screenshot({
    path: "test-results/settings-globs-pr.png",
    fullPage: true,
  });
});

test("settings imports a selected subset from a repository glob", async ({ page }) => {
  await page.goto(`${isolatedServer!.info.base_url}/settings`);
  await page.locator(".settings-page").waitFor({ state: "visible", timeout: 10_000 });

  await page.getByRole("button", { name: "Add repositories…" }).click();
  await expect(page.getByRole("dialog", { name: "Add repositories" })).toBeVisible();
  await page.getByLabel("Repository pattern").fill("roborev-dev/*");
  await page.getByRole("button", { name: "Preview" }).click();

  await expect(page.getByText("roborev-dev/middleman")).toBeVisible();
  await expect(page.getByText("roborev-dev/worker")).toBeVisible();
  await expect(page.getByText("roborev-dev/archived")).toHaveCount(0);

  await page.getByLabel("Filter repositories").fill("worker");
  await page.getByRole("button", { name: "None" }).click();
  await page.getByLabel("Filter repositories").fill("");
  await page.getByRole("button", { name: "Add selected repositories" }).click();

  await expect(page.getByRole("dialog", { name: "Add repositories" })).toHaveCount(0);
  await expect(page.locator(".repo-row", { hasText: "roborev-dev/middleman" })).toBeVisible();
  await expect(page.locator(".repo-row", { hasText: "roborev-dev/worker" })).toHaveCount(0);

  if (!api) throw new Error("settings-globs API context not initialized");
  const response = await api.get("/api/v1/settings");
  const settings = await response.json() as { repos: Array<{ owner: string; name: string; is_glob: boolean }> };
  const exactNames = settings.repos
    .filter((repo) => repo.owner === "roborev-dev" && !repo.is_glob)
    .map((repo) => repo.name)
    .sort();
  expect(exactNames).toEqual(["middleman"]);
});

test("repository import clears stale preview results after failed preview", async ({ page }) => {
  await page.goto(`${isolatedServer!.info.base_url}/settings`);
  await page.locator(".settings-page").waitFor({ state: "visible", timeout: 10_000 });

  await page.getByRole("button", { name: "Add repositories…" }).click();
  const dialog = page.getByRole("dialog", { name: "Add repositories" });
  await dialog.getByLabel("Repository pattern").fill("roborev-dev/*");
  await dialog.getByRole("button", { name: "Preview" }).click();
  await expect(dialog.getByText("roborev-dev/middleman")).toBeVisible();

  await dialog.getByLabel("Repository pattern").fill("bad-owner/[invalid");
  await dialog.getByRole("button", { name: "Preview" }).click();
  await expect(dialog.getByText(/invalid glob pattern|GitHub API error|glob syntax/)).toBeVisible();
  await expect(dialog.getByText("roborev-dev/middleman")).toHaveCount(0);
});

test("repository import ignores older preview responses", async ({ page }) => {
  let firstPreviewRelease: (() => void) | undefined;
  let previewCalls = 0;
  await page.route("**/api/v1/repos/preview", async (route) => {
    previewCalls += 1;
    const request = route.request().postDataJSON() as { owner: string; pattern: string };
    if (previewCalls === 1) {
      await new Promise<void>((resolve) => { firstPreviewRelease = resolve; });
      await route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          owner: request.owner,
          pattern: request.pattern,
          repos: [{
            owner: "roborev-dev",
            name: "middleman",
            description: "Main dashboard",
            private: false,
            pushed_at: "2026-04-22T10:00:00Z",
            already_configured: false,
          }],
        }),
      });
      return;
    }
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        owner: request.owner,
        pattern: request.pattern,
        repos: [{
          owner: "roborev-dev",
          name: "review-bot",
          description: "Review automation",
          private: false,
          pushed_at: "2026-04-24T09:15:00Z",
          already_configured: false,
        }],
      }),
    });
  });

  await page.goto(`${isolatedServer!.info.base_url}/settings`);
  await page.locator(".settings-page").waitFor({ state: "visible", timeout: 10_000 });
  await page.getByRole("button", { name: "Add repositories…" }).click();
  const dialog = page.getByRole("dialog", { name: "Add repositories" });

  await dialog.getByLabel("Repository pattern").fill("roborev-dev/*");
  await dialog.getByRole("button", { name: "Preview" }).click();
  await expect.poll(() => previewCalls).toBe(1);

  await dialog.getByLabel("Repository pattern").fill("roborev-dev/review-*");
  await dialog.getByRole("button", { name: "Preview" }).click();
  await expect.poll(() => previewCalls).toBe(2);
  await expect(dialog.getByText("roborev-dev/review-bot")).toBeVisible();

  firstPreviewRelease?.();
  await expect(dialog.getByText("roborev-dev/review-bot")).toBeVisible();
  await expect(dialog.getByText("roborev-dev/middleman")).toHaveCount(0);
});
