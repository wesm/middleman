import { expect, request as playwrightRequest, test, type APIRequestContext } from "@playwright/test";
import { startIsolatedE2EServer, type IsolatedE2EServer } from "./support/e2eServer";

test.describe("detail action buttons", () => {
  test("issue detail emits direct worktree creation for the issue", async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { activePlatformHost: "github.com" },
        onWorkspaceCommand: (
          cmd: string,
          payload: Record<string, unknown>,
        ) => {
          // eslint-disable-next-line @typescript-eslint/no-explicit-any -- test-only window property
          (window as Record<string, any>).__last_workspace_command = {
            cmd,
            payload,
          };
          return { ok: true };
        },
      };
    });

    await page.goto("/issues/acme/widgets/10");
    await expect(page.locator(".issue-detail")).toBeVisible();

    await page.locator(".btn--workspace").click();

    const command = await page.evaluate(
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- test-only window property
      () => (window as Record<string, any>).__last_workspace_command,
    );
    expect(command).toBeTruthy();
    expect(command.cmd).toBe("createWorktreeFromIssue");
    expect(command.payload.number).toBe(10);
    expect(command.payload.owner).toBe("acme");
    expect(command.payload.name).toBe("widgets");
    expect(command.payload.platformHost).toBe("github.com");
    await expect(page).toHaveURL(/\/issues\/acme\/widgets\/10$/);
  });

  test("issue workspace button still emits worktree creation after detail sync refresh", async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { activePlatformHost: "github.com" },
        onWorkspaceCommand: (
          cmd: string,
          payload: Record<string, unknown>,
        ) => {
          // eslint-disable-next-line @typescript-eslint/no-explicit-any -- test-only window property
          (window as Record<string, any>).__last_workspace_command = {
            cmd,
            payload,
          };
          return { ok: true };
        },
      };
    });

    const syncResponsePromise = page.waitForResponse((response) => {
      const url = response.url();
      return response.request().method() === "POST"
        && url.endsWith("/api/v1/repos/acme/widgets/issues/10/sync");
    });

    await page.goto("/issues/acme/widgets/10");
    await expect(page.locator(".issue-detail")).toBeVisible();

    const syncResponse = await syncResponsePromise;
    expect(syncResponse.status()).toBe(200);
    const syncBody = await syncResponse.json();
    expect(syncBody.platform_host).toBe("github.com");

    await page.locator(".btn--workspace").click();

    const command = await page.evaluate(
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- test-only window property
      () => (window as Record<string, any>).__last_workspace_command,
    );
    expect(command).toBeTruthy();
    expect(command.cmd).toBe("createWorktreeFromIssue");
    expect(command.payload.number).toBe(10);
    expect(command.payload.platformHost).toBe("github.com");
    await expect(page).toHaveURL(/\/issues\/acme\/widgets\/10$/);
  });

  test("pull request actions use shared ActionButton component", async ({ page }) => {
    await page.goto("/pulls");
    await page.locator(".pull-item").first()
      .waitFor({ state: "visible", timeout: 10_000 });
    await page.locator(".pull-item").filter({ hasText: "Add widget caching layer" }).first().click();
    await expect(page.locator(".pull-detail")).toBeVisible();

    const approve = page.locator(".btn--approve");
    const merge = page.locator(".btn--merge");
    const close = page.locator(".btn--close");

    await expect(approve).toBeVisible();
    await expect(merge).toBeVisible();
    await expect(close).toBeVisible();

    // All action buttons use the shared action-button base class
    for (const btn of [approve, merge, close]) {
      const classes = await btn.getAttribute("class");
      expect(classes).toContain("action-button");
    }
  });

  test("draft pull request actions keep exactly the same height", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/6");
    await expect(page.locator(".pull-detail")).toBeVisible();

    const ready = page.locator(".btn--ready");
    const approve = page.locator(".btn--approve");
    const merge = page.locator(".btn--merge");
    const close = page.locator(".btn--close");

    for (const btn of [ready, approve, merge, close]) {
      await expect(btn).toBeVisible();
    }

    const metrics = await page.evaluate(() => {
      const selectors = [".btn--ready", ".btn--approve", ".btn--merge", ".btn--close"];
      return selectors.map((selector) => {
        const element = document.querySelector(selector);
        if (!(element instanceof HTMLElement)) {
          throw new Error(`missing action button: ${selector}`);
        }
        const rect = element.getBoundingClientRect();
        return {
          selector,
          height: element.offsetHeight,
          top: Math.round(rect.top),
          left: Math.round(rect.left),
          right: Math.round(rect.right),
        };
      });
    });

    expect(metrics.map((metric) => metric.height)).toEqual(
      Array(metrics.length).fill(metrics[0]?.height),
    );
    expect(new Set(metrics.map((metric) => metric.top)).size).toBe(1);
    expect(
      metrics.slice(0, -1).map((metric, index) => metrics[index + 1]!.left - metric.right),
    ).toEqual(
      Array(metrics.length - 1).fill(
        metrics[1] ? metrics[1].left - metrics[0]!.right : 0,
      ),
    );
  });

  test("ready for review updates API state and removes the draft action", async ({ page }) => {
    let isolatedServer: IsolatedE2EServer | null = null;
    let api: APIRequestContext | null = null;
    try {
      isolatedServer = await startIsolatedE2EServer();
      api = await playwrightRequest.newContext({
        baseURL: isolatedServer.info.base_url,
      });

      const server = isolatedServer;
      const apiContext = api;

      await page.goto(`${server.info.base_url}/pulls/acme/widgets/6`);
      await expect(page.locator(".pull-detail")).toBeVisible();

      const readyResponsePromise = page.waitForResponse((response) => {
        const url = response.url();
        return response.request().method() === "POST"
          && url === `${server.info.base_url}/api/v1/repos/acme/widgets/pulls/6/ready-for-review`;
      });

      await page.locator(".btn--ready").click();

      const readyResponse = await readyResponsePromise;
      expect(readyResponse.status()).toBe(200);
      expect((await readyResponse.json()).status).toBe("ready_for_review");

      await expect(page.locator(".btn--ready")).toHaveCount(0);
      await expect(page.locator(".btn--approve")).toBeVisible();

      await expect.poll(async () => {
        const response = await apiContext.get("/api/v1/repos/acme/widgets/pulls/6");
        const detail = await response.json();
        return detail.merge_request.IsDraft;
      }).toBe(false);
    } finally {
      await api?.dispose();
      await isolatedServer?.stop();
    }
  });
});
