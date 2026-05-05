import {
  expect,
  request as playwrightRequest,
  test,
  type APIRequestContext,
  type Locator,
} from "@playwright/test";
import {
  startIsolatedWorkspaceE2EServer,
  type IsolatedE2EServer,
} from "./support/e2eServer";

type WorkspaceStatusResponse = {
  id: string;
  status: string;
};

const lockedWorkspaceTestTimeoutMs = 120_000;

type TerminalCanvasStats = {
  hash: string;
  paintedPixels: number;
  width: number;
  height: number;
};

async function readTerminalCanvasStats(
  canvas: Locator,
): Promise<TerminalCanvasStats> {
  return await canvas.evaluate((node) => {
    const terminalCanvas = node as HTMLCanvasElement;
    const context = terminalCanvas.getContext("2d");
    if (!context) {
      throw new Error("terminal canvas 2d context unavailable");
    }

    const { width, height } = terminalCanvas;
    const data = context.getImageData(0, 0, width, height).data;
    let paintedPixels = 0;
    let hash = 0x811c9dc5;
    for (let i = 0; i < data.length; i += 4) {
      const red = data[i] ?? 0;
      const green = data[i + 1] ?? 0;
      const blue = data[i + 2] ?? 0;
      const alpha = data[i + 3] ?? 0;
      if (
        alpha > 0 &&
        Math.abs(red - 0x0d) +
          Math.abs(green - 0x11) +
          Math.abs(blue - 0x17) >
          24
      ) {
        paintedPixels += 1;
      }
      hash ^= red;
      hash = Math.imul(hash, 0x01000193) >>> 0;
      hash ^= green;
      hash = Math.imul(hash, 0x01000193) >>> 0;
      hash ^= blue;
      hash = Math.imul(hash, 0x01000193) >>> 0;
      hash ^= alpha;
      hash = Math.imul(hash, 0x01000193) >>> 0;
    }

    return {
      hash: hash.toString(16),
      paintedPixels,
      width,
      height,
    };
  });
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

test.describe("workspace sidebar full-stack", () => {
  test.describe.configure({ timeout: lockedWorkspaceTestTimeoutMs });

  test("issue workspaces expose the Issue tab and hide Reviews", async ({ page }) => {
    let isolatedServer: IsolatedE2EServer | null = null;
    let api: APIRequestContext | null = null;
    try {
      isolatedServer = await startIsolatedWorkspaceE2EServer();
      api = await playwrightRequest.newContext({
        baseURL: isolatedServer.info.base_url,
      });

      const createResponse = await api.post(
        "/api/v1/issues/github/acme/widgets/10/workspace",
        {
          data: {},
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

  test("ghostty shell terminal paints output and accepts browser keyboard input", async ({ page }) => {
    let isolatedServer: IsolatedE2EServer | null = null;
    let api: APIRequestContext | null = null;
    try {
      isolatedServer = await startIsolatedWorkspaceE2EServer();
      api = await playwrightRequest.newContext({
        baseURL: isolatedServer.info.base_url,
      });
      const settingsResponse = await api.put("/api/v1/settings", {
        data: {
          terminal: {
            font_family: "",
            renderer: "ghostty-web",
          },
        },
      });
      expect(settingsResponse.status()).toBe(200);

      const createResponse = await api.post(
        "/api/v1/issues/github/acme/widgets/10/workspace",
        {
          data: {},
        },
      );
      expect(createResponse.status()).toBe(202);

      const createdWorkspace = await createResponse.json() as WorkspaceStatusResponse;
      await waitForWorkspaceReady(api, createdWorkspace.id);

      await page.goto(
        `${isolatedServer.info.base_url}/terminal/${createdWorkspace.id}`,
      );
      await page
        .getByRole("button", { name: "Open shell drawer" })
        .click();

      const canvas = page.locator(".shell-drawer .terminal-container canvas");
      await expect(canvas).toBeVisible();
      await expect
        .poll(async () => {
          const stats = await readTerminalCanvasStats(canvas);
          return stats.width > 0 && stats.height > 0 && stats.paintedPixels > 0;
        })
        .toBe(true);

      const beforeInput = await readTerminalCanvasStats(canvas);
      await canvas.click({ position: { x: 10, y: 10 } });
      await page.keyboard.type(
        "printf 'MIDDLEMAN_GHOSTTY_E2E_INPUT_REACHED_1234567890'",
      );
      await page.keyboard.press("Enter");

      await expect
        .poll(async () => {
          const stats = await readTerminalCanvasStats(canvas);
          return (
            stats.hash !== beforeInput.hash &&
            Math.abs(stats.paintedPixels - beforeInput.paintedPixels) > 300
          );
        }, { timeout: 10_000 })
        .toBe(true);
    } finally {
      await api?.dispose();
      await isolatedServer?.stop();
    }
  });
});
