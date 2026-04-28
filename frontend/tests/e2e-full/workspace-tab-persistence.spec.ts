import { execFileSync } from "node:child_process";
import { expect, request as playwrightRequest, test, type APIRequestContext } from "@playwright/test";
import { startIsolatedE2EServer, type IsolatedE2EServer } from "./support/e2eServer";

type WorkspaceStatusResponse = {
  id: string;
  status: string;
};

function hasCommand(command: string, args: string[] = ["--version"]): boolean {
  try {
    execFileSync(command, args, { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}

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

async function createIssueWorkspace(
  api: APIRequestContext,
  issueNumber: number,
): Promise<WorkspaceStatusResponse> {
  const createResponse = await api.post(
    `/api/v1/repos/acme/widgets/issues/${issueNumber}/workspace`,
    {
      data: {
        platform_host: "github.com",
      },
    },
  );
  expect(createResponse.status()).toBe(202);
  const createdWorkspace = await createResponse.json() as WorkspaceStatusResponse;
  await waitForWorkspaceReady(api, createdWorkspace.id);
  return createdWorkspace;
}

test.describe("workspace tab persistence", () => {
  test("opening tmux tab keeps Home pane mounted across tab switches", async ({ page }) => {
    test.skip(
      !hasCommand("git") || !hasCommand("tmux", ["-V"]),
      "git and tmux are required for the real workspace flow",
    );

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

      const stage = page.locator(".workspace-stage");
      const panes = stage.locator(":scope > .stage-pane");
      const homeTab = page.locator('.workspace-tabs [role="tab"]', {
        hasText: "Home",
      });
      const tmuxTab = page.locator('.workspace-tabs [role="tab"]', {
        hasText: "tmux",
      });

      // Initial state: only the Home pane is in the stage.
      await expect(homeTab).toHaveAttribute("aria-selected", "true");
      await expect(panes).toHaveCount(1);

      // Open the tmux tab via the Launch menu. The tab is always
      // available because tmux is a built-in launch target.
      await page.locator(".launch-trigger").click();
      await page.locator(".launch-option", { hasText: "tmux" }).click();

      // After opening tmux, both Home and tmux panes should be in
      // the DOM, with tmux marked active.
      await expect(panes).toHaveCount(2);
      await expect(tmuxTab).toHaveAttribute("aria-selected", "true");
      const tmuxPane = stage.locator(":scope > .stage-pane.active");
      await expect(tmuxPane).toHaveCount(1);

      // Mark the tmux pane so we can later confirm it's the same
      // DOM element rather than a fresh remount.
      await tmuxPane.evaluate((el) => {
        el.setAttribute("data-test-tmux-id", "preserved");
      });

      // Switch to Home: tmux pane must remain mounted.
      await homeTab.click();
      await expect(homeTab).toHaveAttribute("aria-selected", "true");
      await expect(panes).toHaveCount(2);
      await expect(
        stage.locator(':scope > .stage-pane[data-test-tmux-id="preserved"]'),
      ).toHaveCount(1);

      // Switch back to tmux: must be the same DOM element, not a
      // freshly mounted one.
      await tmuxTab.click();
      await expect(panes).toHaveCount(2);
      const reactivated = stage.locator(":scope > .stage-pane.active");
      await expect(reactivated).toHaveAttribute("data-test-tmux-id", "preserved");
    } finally {
      await api?.dispose();
      await isolatedServer?.stop();
    }
  });

  test("returns to the most recently selected tab for each workspace", async ({ page }) => {
    test.skip(
      !hasCommand("git") || !hasCommand("tmux", ["-V"]),
      "git and tmux are required for the real workspace flow",
    );

    let isolatedServer: IsolatedE2EServer | null = null;
    let api: APIRequestContext | null = null;
    try {
      isolatedServer = await startIsolatedE2EServer();
      api = await playwrightRequest.newContext({
        baseURL: isolatedServer.info.base_url,
      });

      const firstWorkspace = await createIssueWorkspace(api, 10);
      const secondWorkspace = await createIssueWorkspace(api, 11);

      await page.goto(
        `${isolatedServer.info.base_url}/terminal/${firstWorkspace.id}`,
      );

      const homeTab = page.locator('.workspace-tabs [role="tab"]', {
        hasText: "Home",
      });
      const tmuxTab = page.locator('.workspace-tabs [role="tab"]', {
        hasText: "tmux",
      });

      await expect(homeTab).toHaveAttribute("aria-selected", "true");

      await page.locator(".launch-trigger").click();
      await page.locator(".launch-option", { hasText: "tmux" }).click();
      await expect(tmuxTab).toHaveAttribute("aria-selected", "true");

      await page.goto(
        `${isolatedServer.info.base_url}/terminal/${secondWorkspace.id}`,
      );
      await expect(homeTab).toHaveAttribute("aria-selected", "true");

      await page.goto(
        `${isolatedServer.info.base_url}/terminal/${firstWorkspace.id}`,
      );
      await expect(tmuxTab).toHaveAttribute("aria-selected", "true");
    } finally {
      await api?.dispose();
      await isolatedServer?.stop();
    }
  });
});
