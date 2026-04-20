import { expect, test } from "@playwright/test";

import { mockApi } from "./support/mockApi";

test.beforeEach(async ({ page }) => {
  await mockApi(page);
});

test(
  "panel empty state with noSelection reason",
  async ({ page }) => {
    await page.goto(
      "/workspaces/panel/empty/noSelection",
    );
    await expect(
      page.getByText(
        "Select a worktree to see its pull requests",
      ),
    ).toBeVisible();
  },
);

test(
  "panel empty state with noPlatformRepo reason",
  async ({ page }) => {
    await page.goto(
      "/workspaces/panel/empty/noPlatformRepo",
    );
    await expect(
      page.getByText(
        "This worktree has no linked repository",
      ),
    ).toBeVisible();
  },
);

test(
  "panel non-primary host degraded state",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: {
          activePlatformHost: "github.com",
          panelMode: true,
        },
      };
    });
    await page.goto(
      "/workspaces/panel/example.com/wesm/other-repo",
    );
    await expect(
      page.getByTestId("non-primary-state"),
    ).toBeVisible();
  },
);

test(
  "panel not-ready state when activePlatformHost is null",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: {
          activePlatformHost: null,
          panelMode: true,
        },
      };
    });
    await page.goto(
      "/workspaces/panel/github.com/wesm/other-repo",
    );
    await expect(
      page.getByText("starting up"),
    ).toBeVisible();
  },
);

test(
  "panel list view shows repo header",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { activePlatformHost: "github.com" },
      };
    });
    await page.goto(
      "/workspaces/panel/github.com/acme/widgets",
    );
    await expect(
      page.getByText("acme/widgets"),
    ).toBeVisible();
  },
);

test(
  "panel list view filters pulls by platform host",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { activePlatformHost: "github.com" },
      };
    });
    await page.goto(
      "/workspaces/panel/github.com/acme/widgets",
    );

    // github.com PR visible, example.com PR (same owner/name) filtered out
    await expect(
      page.getByText("Add browser regression coverage"),
    ).toBeVisible();
    await expect(
      page.getByText("Mirror host stub PR"),
    ).toHaveCount(0);
  },
);

test(
  "panel list clicking a PR navigates to detail view",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { activePlatformHost: "github.com" },
      };
    });
    await page.goto(
      "/workspaces/panel/github.com/acme/widgets",
    );

    const row = page
      .locator(".panel-pr-item")
      .filter({ hasText: "Add browser regression coverage" });
    await expect(row).toBeVisible();
    await row.click();

    await expect(page).toHaveURL(
      /\/workspaces\/panel\/github\.com\/acme\/widgets\/42$/,
    );
    await expect(
      page.getByText("#42 Add browser regression coverage"),
    ).toBeVisible();
    await expect(
      page.getByText(
        "Adds Playwright smoke tests for workspace panel.",
      ),
    ).toBeVisible();
  },
);

test(
  "panel detail back button returns to list view",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { activePlatformHost: "github.com" },
      };
    });
    await page.goto(
      "/workspaces/panel/github.com/acme/widgets/42",
    );

    const back = page.getByRole("button", { name: "Back to list" });
    await expect(back).toBeVisible();
    await back.click();

    await expect(page).toHaveURL(
      /\/workspaces\/panel\/github\.com\/acme\/widgets$/,
    );
    await expect(
      page.locator(".panel-pr-item").filter({
        hasText: "Add browser regression coverage",
      }),
    ).toBeVisible();
  },
);

test(
  "panel + Worktree button emits createWorktreeFromPR",
  async ({ page }) => {
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
    await page.goto(
      "/workspaces/panel/github.com/acme/widgets",
    );

    const createBtn = page.locator("button.create-wt-btn").first();
    await expect(createBtn).toBeVisible();
    await createBtn.click();

    const command = await page.evaluate(
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- test-only window property
      () => (window as Record<string, any>).__last_workspace_command,
    );
    expect(command).toBeTruthy();
    expect(command.cmd).toBe("createWorktreeFromPR");
    expect(command.payload.number).toBe(42);
    expect(command.payload.owner).toBe("acme");
    expect(command.payload.name).toBe("widgets");
    expect(command.payload.platformHost).toBe("github.com");
  },
);

test(
  "panel + Worktree button activates via keyboard without row navigation",
  async ({ page }) => {
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
    await page.goto(
      "/workspaces/panel/github.com/acme/widgets",
    );

    const createBtn = page.locator("button.create-wt-btn").first();
    await expect(createBtn).toBeVisible();
    await createBtn.focus();
    await page.keyboard.press("Enter");

    // URL stays on list view — row keydown handler must not hijack the button
    await expect(page).toHaveURL(
      /\/workspaces\/panel\/github\.com\/acme\/widgets$/,
    );

    const command = await page.evaluate(
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- test-only window property
      () => (window as Record<string, any>).__last_workspace_command,
    );
    expect(command).toBeTruthy();
    expect(command.cmd).toBe("createWorktreeFromPR");
    expect(command.payload.number).toBe(42);
  },
);

test(
  "panel detail not-found keeps Back button and shows Refresh",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { activePlatformHost: "github.com" },
        onWorkspaceCommand: () => ({ ok: true }),
      };
    });
    await page.goto(
      "/workspaces/panel/github.com/acme/widgets/999",
    );

    await expect(
      page.getByTestId("detail-not-found"),
    ).toBeVisible();
    await expect(
      page.getByTestId("detail-not-found"),
    ).toContainText("#999");
    await expect(
      page.getByRole("button", { name: "Back to list" }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Refresh" }),
    ).toBeVisible();
  },
);

test(
  "panel detail fallback fetches via single-PR endpoint",
  async ({ page }) => {
    // Mock a single-PR endpoint for a closed PR not in /pulls
    await page.route(
      "**/api/v1/repos/acme/widgets/pulls/100",
      async (route) => {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            merge_request: {
              ID: 100,
              Number: 100,
              Title: "Closed feature",
              Author: "alice",
              State: "closed",
              repo_owner: "acme",
              repo_name: "widgets",
              worktree_links: [],
            },
            repo_owner: "acme",
            repo_name: "widgets",
            detail_loaded: true,
            detail_fetched_at: "2026-04-10T00:00:00Z",
            worktree_links: [],
          }),
        });
      },
    );

    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { activePlatformHost: "github.com" },
        onWorkspaceCommand: () => ({ ok: true }),
      };
    });
    await page.goto(
      "/workspaces/panel/github.com/acme/widgets/100",
    );

    // Should render the fallback-fetched PR detail
    await expect(
      page.getByText("Closed feature"),
    ).toBeVisible();
    await expect(
      page.getByText("#100"),
    ).toBeVisible();
  },
);

test(
  "panel detail Refresh retries single-PR fetch after miss",
  async ({ page }) => {
    let fetchCount = 0;
    await page.route(
      "**/api/v1/repos/acme/widgets/pulls/200",
      async (route) => {
        fetchCount++;
        if (fetchCount === 1) {
          await route.fulfill({
            status: 404,
            contentType: "application/json",
            body: JSON.stringify({ error: "Not found" }),
          });
        } else {
          await route.fulfill({
            status: 200,
            contentType: "application/json",
            body: JSON.stringify({
              merge_request: {
                ID: 200,
                Number: 200,
                Title: "Late-synced PR",
                Author: "bob",
                State: "open",
                repo_owner: "acme",
                repo_name: "widgets",
                worktree_links: [],
              },
              repo_owner: "acme",
              repo_name: "widgets",
              detail_loaded: true,
              detail_fetched_at: "2026-04-10T00:00:00Z",
              worktree_links: [],
            }),
          });
        }
      },
    );

    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { activePlatformHost: "github.com" },
        onWorkspaceCommand: () => ({ ok: true }),
      };
    });
    await page.goto(
      "/workspaces/panel/github.com/acme/widgets/200",
    );

    // First fetch returns 404 — shows not-found
    await expect(
      page.getByTestId("detail-not-found"),
    ).toBeVisible();

    // Click Refresh — resets dedup guard and retries
    await page.getByRole("button", { name: "Refresh" }).click();

    // Second fetch returns PR — shows detail
    await expect(
      page.getByText("Late-synced PR"),
    ).toBeVisible();
  },
);

test(
  "panel detail shows error state on server failure",
  async ({ page }) => {
    await page.route(
      "**/api/v1/repos/acme/widgets/pulls/500",
      async (route) => {
        await route.fulfill({
          status: 500,
          contentType: "application/json",
          body: JSON.stringify({ error: "Internal error" }),
        });
      },
    );

    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { activePlatformHost: "github.com" },
        onWorkspaceCommand: () => ({ ok: true }),
      };
    });
    await page.goto(
      "/workspaces/panel/github.com/acme/widgets/500",
    );

    await expect(
      page.getByTestId("detail-error"),
    ).toBeVisible();
    await expect(
      page.getByText("Failed to load PR #500"),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Retry" }),
    ).toBeVisible();
  },
);

test(
  "panel select PR navigates to detail",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { activePlatformHost: "github.com" },
        onWorkspaceCommand: () => ({ ok: true }),
      };
    });
    await page.goto(
      "/workspaces/panel/github.com/acme/widgets",
    );

    const row = page
      .locator(".panel-pr-item")
      .filter({ hasText: "Add browser regression coverage" });
    await row.click();

    await expect(page).toHaveURL(
      /\/workspaces\/panel\/github\.com\/acme\/widgets\/42$/,
    );
  },
);

test(
  "panel back returns to list",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { activePlatformHost: "github.com" },
        onWorkspaceCommand: () => ({ ok: true }),
      };
    });
    await page.goto(
      "/workspaces/panel/github.com/acme/widgets",
    );
    await page
      .locator(".panel-pr-item")
      .filter({ hasText: "Add browser regression coverage" })
      .click();

    await page
      .getByRole("button", { name: "Back to list" })
      .click();

    await expect(page).toHaveURL(
      /\/workspaces\/panel\/github\.com\/acme\/widgets$/,
    );
  },
);

test(
  "panel worktree chip emits navigateWorktree",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { activePlatformHost: "github.com" },
        onWorkspaceCommand: (
          cmd: string,
          payload: Record<string, unknown>,
        ) => {
          (window as Record<string, unknown>).__last_cmd = {
            cmd,
            payload,
          };
          return { ok: true };
        },
      };
    });
    await page.goto(
      "/workspaces/panel/github.com/acme/widgets",
    );

    const row = page
      .locator(".panel-pr-item")
      .filter({ hasText: "Refactor theme system" });
    await row.hover();

    const chip = row.locator("button.wt-chip");
    await expect(chip).toBeVisible();
    await expect(chip).toHaveText("theme-rework");
    await chip.click();

    const cmd = await page.evaluate(
      () => (window as Record<string, unknown>).__last_cmd as
        { cmd: string; payload: Record<string, unknown> },
    );
    expect(cmd.cmd).toBe("navigateWorktree");
    expect(cmd.payload.worktreeKey).toBe(
      "projects/theme-rework",
    );

    // Should stay on list (chip click stops propagation)
    await expect(page).toHaveURL(
      /\/workspaces\/panel\/github\.com\/acme\/widgets$/,
    );
  },
);

test(
  "panel non-primary state shows both hosts and Reveal button",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: {
          activePlatformHost: "github.com",
          panelMode: true,
        },
        onWorkspaceCommand: (
          cmd: string,
        ) => {
          (window as Record<string, unknown>).__last_cmd = {
            cmd,
          };
          return { ok: true };
        },
      };
    });
    await page.goto(
      "/workspaces/panel/example.com/wesm/other-repo",
    );

    const state = page.getByTestId("non-primary-state");
    await expect(state).toBeVisible();
    await expect(state).toContainText("example.com");
    await expect(state).toContainText("github.com");

    const reveal = page.getByRole("button", {
      name: "Reveal in Host Settings",
    });
    await expect(reveal).toBeVisible();
    await reveal.click();

    const cmd = await page.evaluate(
      () => (window as Record<string, unknown>).__last_cmd as
        { cmd: string },
    );
    expect(cmd.cmd).toBe("revealHostSettings");
  },
);
