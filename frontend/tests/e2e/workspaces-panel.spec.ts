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
        embed: { activePlatformHost: "github.com" },
      };
    });
    await page.goto(
      "/workspaces/panel/example.com/wesm/ghosthub",
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
        embed: { activePlatformHost: null },
      };
    });
    await page.goto(
      "/workspaces/panel/github.com/wesm/ghosthub",
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
  "panel detail not-found when PR missing from store",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { activePlatformHost: "github.com" },
      };
    });
    // PR #999 is not in the mock fixture
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
  },
);
