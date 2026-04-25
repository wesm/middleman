import { execFileSync } from "node:child_process";
import { access } from "node:fs/promises";
import { expect, request as playwrightRequest, test, type APIRequestContext } from "@playwright/test";
import { startIsolatedE2EServer, type IsolatedE2EServer } from "./support/e2eServer";

type WorkspaceStatusResponse = {
  id: string;
  platform_host: string;
  repo_owner: string;
  repo_name: string;
  item_type: string;
  item_number: number;
  git_head_ref: string;
  worktree_path: string;
  tmux_session: string;
  status: string;
  error_message?: string | null;
};

function hasCommand(command: string, args: string[] = ["--version"]): boolean {
  try {
    execFileSync(command, args, { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}

function gitOutput(dir: string, args: string[]): string {
  return execFileSync("git", args, {
    cwd: dir,
    encoding: "utf8",
  }).trim();
}

async function waitForWorkspaceReady(
  api: APIRequestContext,
  workspaceId: string,
): Promise<WorkspaceStatusResponse> {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    const response = await api.get(`/api/v1/workspaces/${workspaceId}`);
    expect(response.ok()).toBe(true);
    const workspace = await response.json() as WorkspaceStatusResponse;
    if (workspace.status === "ready") {
      return workspace;
    }
    if (workspace.status === "error") {
      throw new Error(
        workspace.error_message ?? `workspace ${workspaceId} failed to become ready`,
      );
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }

  throw new Error(`workspace ${workspaceId} did not become ready`);
}

test.describe("detail action buttons", () => {
  test("issue detail creates a middleman workspace and opens its terminal", async ({ page }) => {
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

      const server = isolatedServer;
      const apiContext = api;

      await page.goto(`${server.info.base_url}/issues/acme/widgets/10`);
      await expect(page.locator(".issue-detail")).toBeVisible();

      const createResponsePromise = page.waitForResponse((response) => {
        const url = response.url();
        return response.request().method() === "POST"
          && url === `${server.info.base_url}/api/v1/repos/acme/widgets/issues/10/workspace`;
      });

      await page.locator(".btn--workspace").click();

      const createResponse = await createResponsePromise;
      expect(createResponse.status()).toBe(202);

      const createdWorkspace = await createResponse.json() as WorkspaceStatusResponse;
      expect(createdWorkspace.platform_host).toBe("github.com");
      expect(createdWorkspace.item_type).toBe("issue");
      expect(createdWorkspace.item_number).toBe(10);
      expect(createdWorkspace.git_head_ref).toBe("middleman/issue-10");

      await expect(page).toHaveURL(
        new RegExp(`/terminal/${createdWorkspace.id}$`),
      );

      const readyWorkspace = await waitForWorkspaceReady(
        apiContext,
        createdWorkspace.id,
      );
      await access(readyWorkspace.worktree_path);
      expect(
        gitOutput(readyWorkspace.worktree_path, ["branch", "--show-current"]),
      ).toBe("middleman/issue-10");
    } finally {
      await api?.dispose();
      await isolatedServer?.stop();
    }
  });

  test("issue workspace button still creates a middleman workspace after detail sync refresh", async ({ page }) => {
    const createdWorkspace = {
      id: "ws-issue-10",
      platform_host: "github.com",
      repo_owner: "acme",
      repo_name: "widgets",
      item_type: "issue",
      item_number: 10,
      git_head_ref: "middleman/issue-10",
      worktree_path: "/tmp/workspaces/issue-10",
      tmux_session: "middleman-ws-issue-10",
      status: "ready",
      created_at: "2026-04-20T12:00:00Z",
      mr_title: "Add keyboard shortcut docs",
      mr_state: "open",
    };
    let createCalls = 0;
    await page.route(
      "**/api/v1/repos/acme/widgets/issues/10/workspace",
      async (route) => {
        createCalls += 1;
        await route.fulfill({
          status: 202,
          contentType: "application/json",
          body: JSON.stringify(createdWorkspace),
        });
      },
    );
    await page.route(
      "**/api/v1/workspaces/ws-issue-10",
      async (route) => {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(createdWorkspace),
        });
      },
    );
    await page.route(
      "**/api/v1/workspaces",
      async (route) => {
        if (route.request().method() !== "GET") {
          await route.continue();
          return;
        }
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ workspaces: [createdWorkspace] }),
        });
      },
    );
    await page.route(
      "**/api/v1/events",
      async (route) => {
        await route.fulfill({
          status: 200,
          contentType: "text/event-stream",
          body: "",
        });
      },
    );

    const syncResponsePromise = page.waitForResponse((response) => {
      const url = response.url();
      return response.request().method() === "POST"
        && url.endsWith("/api/v1/repos/acme/widgets/issues/10/sync");
    });
    const createResponsePromise = page.waitForResponse((response) => {
      const url = response.url();
      return response.request().method() === "POST"
        && url.endsWith("/api/v1/repos/acme/widgets/issues/10/workspace");
    });

    await page.goto("/issues/acme/widgets/10");
    await expect(page.locator(".issue-detail")).toBeVisible();

    const syncResponse = await syncResponsePromise;
    expect(syncResponse.status()).toBe(200);
    const syncBody = await syncResponse.json();
    expect(syncBody.platform_host).toBe("github.com");

    await page.locator(".btn--workspace").click();
    const createResponse = await createResponsePromise;
    expect(createResponse.status()).toBe(202);
    expect(createCalls).toBe(1);
    await expect(page).toHaveURL(/\/terminal\/ws-issue-10$/);
  });

  test("issue workspace conflict dialog can reuse the existing branch", async ({ page }) => {
    const createdWorkspace = {
      id: "ws-issue-10",
      platform_host: "github.com",
      repo_owner: "acme",
      repo_name: "widgets",
      item_type: "issue",
      item_number: 10,
      git_head_ref: "middleman/issue-10",
      worktree_path: "/tmp/workspaces/issue-10",
      tmux_session: "middleman-ws-issue-10",
      status: "ready",
      created_at: "2026-04-20T12:00:00Z",
      mr_title: "Add keyboard shortcut docs",
      mr_state: "open",
    };
    const conflict = {
      type: "urn:middleman:error:issue-workspace-branch-conflict",
      title: "Issue workspace branch conflict",
      status: 409,
      detail: "A local branch with the requested name already exists.",
      errors: [
        {
          message: "Requested branch already exists",
          location: "body.git_head_ref",
          value: "middleman/issue-10",
        },
        {
          message: "Suggested alternative branch name",
          location: "body.suggested_git_head_ref",
          value: "middleman/issue-10-2",
        },
      ],
    };

    const payloads: Record<string, unknown>[] = [];
    await page.route(
      "**/api/v1/repos/acme/widgets/issues/10/workspace",
      async (route) => {
        payloads.push(JSON.parse(
          route.request().postData() ?? "{}",
        ) as Record<string, unknown>);
        if (payloads.length === 1) {
          await route.fulfill({
            status: 409,
            contentType: "application/problem+json",
            body: JSON.stringify(conflict),
          });
          return;
        }
        await route.fulfill({
          status: 202,
          contentType: "application/json",
          body: JSON.stringify(createdWorkspace),
        });
      },
    );
    await page.route(
      "**/api/v1/workspaces/ws-issue-10",
      async (route) => {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(createdWorkspace),
        });
      },
    );
    await page.route(
      "**/api/v1/workspaces",
      async (route) => {
        if (route.request().method() !== "GET") {
          await route.continue();
          return;
        }
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ workspaces: [createdWorkspace] }),
        });
      },
    );
    await page.route(
      "**/api/v1/events",
      async (route) => {
        await route.fulfill({
          status: 200,
          contentType: "text/event-stream",
          body: "",
        });
      },
    );

    await page.goto("/issues/acme/widgets/10");
    await expect(page.locator(".issue-detail")).toBeVisible();

    await page.locator(".btn--workspace").click();

    const dialog = page.getByRole("dialog", { name: "Branch Name Conflict" });
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("middleman/issue-10");
    await expect(
      dialog.locator("#issue-workspace-branch-name"),
    ).toHaveValue("middleman/issue-10-2");

    await dialog.getByRole("button", { name: "Use Existing Branch" }).click();

    await expect.poll(() => payloads).toEqual([
      {
        platform_host: "github.com",
      },
      {
        platform_host: "github.com",
        git_head_ref: "middleman/issue-10",
        reuse_existing_branch: true,
      },
    ]);
    await expect(page).toHaveURL(/\/terminal\/ws-issue-10$/);
  });

  test("issue workspace conflict dialog can create a new suggested branch", async ({ page }) => {
    const createdWorkspace = {
      id: "ws-issue-10",
      platform_host: "github.com",
      repo_owner: "acme",
      repo_name: "widgets",
      item_type: "issue",
      item_number: 10,
      git_head_ref: "middleman/issue-10-2",
      worktree_path: "/tmp/workspaces/issue-10-2",
      tmux_session: "middleman-ws-issue-10",
      status: "ready",
      created_at: "2026-04-20T12:00:00Z",
      mr_title: "Add keyboard shortcut docs",
      mr_state: "open",
    };
    const conflict = {
      type: "urn:middleman:error:issue-workspace-branch-conflict",
      title: "Issue workspace branch conflict",
      status: 409,
      detail: "A local branch with the requested name already exists.",
      errors: [
        {
          message: "Requested branch already exists",
          location: "body.git_head_ref",
          value: "middleman/issue-10",
        },
        {
          message: "Suggested alternative branch name",
          location: "body.suggested_git_head_ref",
          value: "middleman/issue-10-2",
        },
      ],
    };

    const payloads: Record<string, unknown>[] = [];
    await page.route(
      "**/api/v1/repos/acme/widgets/issues/10/workspace",
      async (route) => {
        payloads.push(JSON.parse(
          route.request().postData() ?? "{}",
        ) as Record<string, unknown>);
        if (payloads.length === 1) {
          await route.fulfill({
            status: 409,
            contentType: "application/problem+json",
            body: JSON.stringify(conflict),
          });
          return;
        }
        await route.fulfill({
          status: 202,
          contentType: "application/json",
          body: JSON.stringify(createdWorkspace),
        });
      },
    );
    await page.route(
      "**/api/v1/workspaces/ws-issue-10",
      async (route) => {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(createdWorkspace),
        });
      },
    );
    await page.route(
      "**/api/v1/workspaces",
      async (route) => {
        if (route.request().method() !== "GET") {
          await route.continue();
          return;
        }
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ workspaces: [createdWorkspace] }),
        });
      },
    );
    await page.route(
      "**/api/v1/events",
      async (route) => {
        await route.fulfill({
          status: 200,
          contentType: "text/event-stream",
          body: "",
        });
      },
    );

    await page.goto("/issues/acme/widgets/10");
    await expect(page.locator(".issue-detail")).toBeVisible();

    await page.locator(".btn--workspace").click();

    const dialog = page.getByRole("dialog", { name: "Branch Name Conflict" });
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Create New Branch" }).click();

    await expect.poll(() => payloads).toEqual([
      {
        platform_host: "github.com",
      },
      {
        platform_host: "github.com",
        git_head_ref: "middleman/issue-10-2",
      },
    ]);
    await expect(page).toHaveURL(/\/terminal\/ws-issue-10$/);
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
