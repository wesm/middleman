import { expect, request as playwrightRequest, test, type APIRequestContext } from "@playwright/test";
import { startIsolatedE2EServer, type IsolatedE2EServer } from "./support/e2eServer";

type RepoSummary = {
  Owner: string;
  Name: string;
};

let isolatedServer: IsolatedE2EServer | undefined;
let api: APIRequestContext | undefined;

test.beforeAll(async () => {
  isolatedServer = await startIsolatedE2EServer();
  api = await playwrightRequest.newContext({
    baseURL: isolatedServer.info.base_url,
  });
});

test.afterAll(async () => {
  await api?.dispose();
  await isolatedServer?.stop();
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
