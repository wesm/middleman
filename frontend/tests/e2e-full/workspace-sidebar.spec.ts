import { expect, request as playwrightRequest, test, type APIRequestContext } from "@playwright/test";
import { startIsolatedE2EServer, type IsolatedE2EServer } from "./support/e2eServer";

type WorkspaceStatusResponse = {
  id: string;
  status: string;
};

async function waitForWorkspaceReady(
  api: APIRequestContext,
  workspaceId: string,
): Promise<void> {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    const response = await api.get(`/api/v1/workspaces/${workspaceId}`);
    expect(response.ok()).toBe(true);
    const workspace = await response.json() as WorkspaceStatusResponse;
    if (workspace.status === "ready") {
      return;
    }
    if (workspace.status === "error") {
      throw new Error(`workspace ${workspaceId} failed to become ready`);
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }

  throw new Error(`workspace ${workspaceId} did not become ready`);
}

test.describe("workspace sidebar full-stack", () => {
  test("issue workspaces expose the Issue tab and hide Reviews", async ({ page }) => {
    let isolatedServer: IsolatedE2EServer | null = null;
    let api: APIRequestContext | null = null;
    try {
      isolatedServer = await startIsolatedE2EServer();
      api = await playwrightRequest.newContext({
        baseURL: isolatedServer.info.base_url,
      });

      const createResponse = await api.post(
        "/api/v1/repos/acme/widgets/issues/10/workspace",
        {
          data: {
            platform_host: "github.com",
          },
        },
      );
      expect(createResponse.status()).toBe(202);

      const createdWorkspace = await createResponse.json() as WorkspaceStatusResponse;
      await waitForWorkspaceReady(api, createdWorkspace.id);

      await page.goto(
        `${isolatedServer.info.base_url}/terminal/${createdWorkspace.id}`,
      );

      await expect(
        page.locator(".terminal-view .seg-btn", { hasText: "Issue" }),
      ).toBeVisible();
      await expect(
        page.locator(".terminal-view .seg-btn", { hasText: "PR" }),
      ).toHaveCount(0);
      await expect(
        page.locator(".terminal-view .seg-btn", { hasText: "Reviews" }),
      ).toHaveCount(0);

      await page.locator(".terminal-view .seg-btn", { hasText: "Issue" }).click();
      await expect(page.locator(".right-sidebar")).toBeVisible();
      await expect(
        page.locator(".right-sidebar .detail-title"),
      ).toContainText("Widget rendering broken on Safari");
    } finally {
      await api?.dispose();
      await isolatedServer?.stop();
    }
  });
});
