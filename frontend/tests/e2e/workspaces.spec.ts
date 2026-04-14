import { expect, test } from "@playwright/test";

import { mockApi } from "./support/mockApi";

const testWorkspaceData: WorkspaceData = {
  hosts: [{
    key: "local",
    label: "Local",
    connectionState: "connected",
    transport: "local",
    platform: "macOS",
    projects: [{
      key: "proj-1",
      name: "test-project",
      kind: "repository",
      repoKind: "STANDARD",
      defaultBranch: "main",
      platformRepo: "acme/test-project",
      worktrees: [
        {
          key: "wt-1",
          name: "main",
          branch: "main",
          isPrimary: true,
          isHidden: false,
          isStale: false,
          sessionBackend: "local",
          linkedPR: null,
          activity: { state: "idle", lastOutputAt: null },
          diff: null,
        },
        {
          key: "wt-2",
          name: "feature-auth",
          branch: "feature/auth",
          isPrimary: false,
          isHidden: false,
          isStale: false,
          sessionBackend: "local",
          linkedPR: {
            number: 42,
            title: "Add auth middleware",
            state: "open",
            checksStatus: "success",
            updatedAt: "2026-04-10T12:00:00Z",
          },
          activity: {
            state: "active",
            lastOutputAt: "2026-04-10T12:00:00Z",
          },
          diff: { added: 45, removed: 12 },
        },
      ],
    }],
    sessions: [],
    resources: null,
  }],
  selectedWorktreeKey: "wt-1",
  selectedHostKey: "local",
};

const primaryHost = testWorkspaceData.hosts[0]!;
const primaryProject = primaryHost.projects[0]!;

test.beforeEach(async ({ page }) => {
  await mockApi(page);
});

test("workspaces route renders empty state", async ({ page }) => {
  await page.goto("/workspaces");
  await expect(
    page.getByText("No workspace data available"),
  ).toBeVisible();
});

test("AppHeader shows Workspaces tab", async ({ page }) => {
  await page.addInitScript((d) => {
    window.__middleman_config = { workspace: d };
  }, testWorkspaceData);
  await page.goto("/pulls");
  await expect(
    page.getByRole("button", { name: "Workspaces" }),
  ).toBeVisible();
});

test(
  "Workspaces tab navigates to /workspaces",
  async ({ page }) => {
    await page.addInitScript((d) => {
      window.__middleman_config = { workspace: d };
    }, testWorkspaceData);
    await page.goto("/pulls");
    await page
      .getByRole("button", { name: "Workspaces" })
      .click();
    await expect(page).toHaveURL(/\/workspaces/);
  },
);

test(
  "hideHeader suppresses AppHeader",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { hideHeader: true },
      };
    });
    await page.goto("/workspaces");
    await expect(page.locator("header.app-header")).toHaveCount(0);
  },
);

test(
  "workspace data injection renders sidebar",
  async ({ page }) => {
    const data = testWorkspaceData;
    await page.addInitScript((d) => {
      window.__middleman_config = { workspace: d };
    }, data);
    await page.goto("/workspaces");
    await expect(
      page.locator(".project-name", { hasText: "test-project" }),
    ).toBeVisible();
    await expect(
      page.getByText("Add auth middleware"),
    ).toBeVisible();
  },
);

test(
  "single host renders the host chip with transport and platform",
  async ({ page }) => {
    await page.addInitScript((d) => {
      window.__middleman_config = { workspace: d };
    }, testWorkspaceData);
    await page.goto("/workspaces");
    const chip = page.getByTestId("single-host-chip");
    await expect(chip).toBeVisible();
    await expect(chip).toContainText("Local");
    await expect(chip.locator(".transport-badge")).toContainText("LOCAL");
    await expect(chip.locator(".platform-icon")).toBeVisible();
  },
);

test(
  "bridge update method renders workspace data",
  async ({ page }) => {
    // Start with embedded config but no workspace data
    await page.addInitScript(() => {
      window.__middleman_config = {};
    });
    await page.goto("/workspaces");
    await expect(
      page.getByText("No workspace data available"),
    ).toBeVisible();

    const data = testWorkspaceData;
    await page.evaluate((d) => {
      window.__middleman_update_workspace?.(d as WorkspaceData);
    }, data);

    await expect(
      page.locator(".project-name", { hasText: "test-project" }),
    ).toBeVisible();
    await expect(
      page.getByText("Add auth middleware"),
    ).toBeVisible();
  },
);

test(
  "clicking PR badge emits pinLinkedPR command",
  async ({ page }) => {
    await page.addInitScript((data) => {
      window.__middleman_config = {
        workspace: data,
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
    }, testWorkspaceData);

    await page.goto("/workspaces");

    const prBadge = page.locator("button.pr-badge").first();
    await expect(prBadge).toBeVisible();
    await prBadge.click();

    const command = await page.evaluate(
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- test-only window property
      () => (window as Record<string, any>).__last_workspace_command,
    );
    expect(command).toBeTruthy();
    expect(command.cmd).toBe("pinLinkedPR");
    expect(command.payload.hostKey).toBe("local");
    expect(command.payload.projectKey).toBe("proj-1");
    expect(command.payload.worktreeKey).toBe("wt-2");
    expect(command.payload.prNumber).toBe(42);
  },
);

test(
  "navigateToRoute bridge method works",
  async ({ page }) => {
    await page.goto("/pulls");
    await page.evaluate(() => {
      window.__middleman_navigate_to_route?.("/workspaces");
    });
    await expect(page).toHaveURL(/\/workspaces/);
  },
);

// --- New sidebar interaction tests ---

/**
 * Helper: inject workspace data with onWorkspaceCommand callback
 * that records the last emitted command on window.__last_workspace_command.
 */
async function injectWithCallback(
  page: import("@playwright/test").Page,
  data: WorkspaceData,
): Promise<void> {
  await page.addInitScript((d) => {
    window.__middleman_config = {
      workspace: d,
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
  }, data);
}

async function getLastCommand(
  page: import("@playwright/test").Page,
): Promise<{ cmd: string; payload: Record<string, unknown> }> {
  return page.evaluate(
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- test-only window property
    () => (window as Record<string, any>).__last_workspace_command,
  );
}

test(
  "clicking worktree row emits selectWorktree",
  async ({ page }) => {
    await injectWithCallback(page, testWorkspaceData);
    await page.goto("/workspaces");

    const row = page
      .locator(".worktree-row")
      .filter({ hasText: "Add auth middleware" });
    await expect(row).toBeVisible();
    await row.click();

    const command = await getLastCommand(page);
    expect(command).toBeTruthy();
    expect(command.cmd).toBe("selectWorktree");
    expect(command.payload.worktreeKey).toBe("wt-2");
    expect(command.payload.hostKey).toBe("local");
    expect(command.payload.projectKey).toBe("proj-1");
  },
);

test(
  "worktree context menu: deleteWorktree",
  async ({ page }) => {
    await injectWithCallback(page, testWorkspaceData);
    await page.goto("/workspaces");

    const row = page
      .locator(".worktree-row")
      .filter({ hasText: "Add auth middleware" });
    await expect(row).toBeVisible();
    await row.click({ button: "right" });

    const menuItem = page.getByText("Delete worktree");
    await expect(menuItem).toBeVisible();
    await menuItem.click();

    const command = await getLastCommand(page);
    expect(command).toBeTruthy();
    expect(command.cmd).toBe("deleteWorktree");
    expect(command.payload.hostKey).toBe("local");
    expect(command.payload.projectKey).toBe("proj-1");
    expect(command.payload.worktreeKey).toBe("wt-2");
  },
);

test(
  "worktree context menu: setWorktreeHidden",
  async ({ page }) => {
    await injectWithCallback(page, testWorkspaceData);
    await page.goto("/workspaces");

    const row = page
      .locator(".worktree-row")
      .filter({ hasText: "Add auth middleware" });
    await expect(row).toBeVisible();
    await row.click({ button: "right" });

    // wt-2 has isHidden: false, so menu shows "Hide worktree"
    const menuItem = page.getByText("Hide worktree");
    await expect(menuItem).toBeVisible();
    await menuItem.click();

    const command = await getLastCommand(page);
    expect(command).toBeTruthy();
    expect(command.cmd).toBe("setWorktreeHidden");
    expect(command.payload.hostKey).toBe("local");
    expect(command.payload.projectKey).toBe("proj-1");
    expect(command.payload.worktreeKey).toBe("wt-2");
    expect(command.payload.hidden).toBe(true);
  },
);

test(
  "activity state dot colors match activity state",
  async ({ page }) => {
    const activityData = {
      ...testWorkspaceData,
      hosts: [{
        ...primaryHost,
        projects: [{
          ...primaryProject,
          worktrees: [
            {
              key: "wt-idle",
              name: "idle-wt",
              branch: "idle",
              isPrimary: false,
              isHidden: false,
              isStale: false,
              sessionBackend: "local",
              linkedPR: null,
              activity: { state: "idle", lastOutputAt: null },
              diff: null,
            },
            {
              key: "wt-active",
              name: "active-wt",
              branch: "active",
              isPrimary: false,
              isHidden: false,
              isStale: false,
              sessionBackend: "local",
              linkedPR: null,
              activity: {
                state: "active",
                lastOutputAt: "2026-04-10T12:00:00Z",
              },
              diff: null,
            },
            {
              key: "wt-running",
              name: "running-wt",
              branch: "running",
              isPrimary: false,
              isHidden: false,
              isStale: false,
              sessionBackend: "local",
              linkedPR: null,
              activity: {
                state: "running",
                lastOutputAt: "2026-04-10T12:00:00Z",
              },
              diff: null,
            },
            {
              key: "wt-attention",
              name: "attention-wt",
              branch: "attention",
              isPrimary: false,
              isHidden: false,
              isStale: false,
              sessionBackend: "local",
              linkedPR: null,
              activity: {
                state: "needsAttention",
                lastOutputAt: "2026-04-10T12:00:00Z",
              },
              diff: null,
            },
          ],
        }],
      }],
    } satisfies WorkspaceData;

    await page.addInitScript((d) => {
      window.__middleman_config = { workspace: d };
    }, activityData);
    await page.goto("/workspaces");

    const expected: [string, string][] = [
      ["idle-wt", "var(--text-muted)"],
      ["active-wt", "var(--accent-green)"],
      ["running-wt", "var(--accent-blue)"],
      ["attention-wt", "var(--accent-amber)"],
    ];

    for (const [name, cssVar] of expected) {
      const dot = page
        .locator(".worktree-row")
        .filter({ hasText: name })
        .locator(".activity-dot");
      await expect(dot).toBeVisible();
      const style = await dot.getAttribute("style");
      expect(style).toContain(`background: ${cssVar}`);
    }
  },
);

test(
  "selected worktree row has .selected class",
  async ({ page }) => {
    await page.addInitScript((d) => {
      window.__middleman_config = { workspace: d };
    }, testWorkspaceData);
    await page.goto("/workspaces");

    // wt-1 ("main") is selectedWorktreeKey
    const selectedRow = page
      .locator(".worktree-row")
      .filter({ hasText: "main" });
    await expect(selectedRow).toBeVisible();
    await expect(selectedRow).toHaveClass(/selected/);

    // wt-2 (renders as "Add auth middleware") should NOT be selected
    const otherRow = page
      .locator(".worktree-row")
      .filter({ hasText: "Add auth middleware" });
    await expect(otherRow).toBeVisible();
    await expect(otherRow).not.toHaveClass(/selected/);
  },
);

test(
  "project collapse and expand toggles worktree rows",
  async ({ page }) => {
    await page.addInitScript((d) => {
      window.__middleman_config = { workspace: d };
    }, testWorkspaceData);
    await page.goto("/workspaces");

    const worktreeRow = page.getByText("Add auth middleware");
    await expect(worktreeRow).toBeVisible();

    // Collapse by clicking project header
    const header = page.locator(".project-header").first();
    await header.click();
    await expect(worktreeRow).not.toBeVisible();

    // Expand again
    await header.click();
    await expect(worktreeRow).toBeVisible();
  },
);

test(
  "Add Repository button emits addRepository command",
  async ({ page }) => {
    await injectWithCallback(page, testWorkspaceData);
    await page.goto("/workspaces");

    const addBtn = page.locator("button.add-repo-btn");
    await expect(addBtn).toBeVisible();
    await addBtn.click();

    const command = await getLastCommand(page);
    expect(command).toBeTruthy();
    expect(command.cmd).toBe("addRepository");
    expect(command.payload).toEqual({ hostKey: "local" });
  },
);

test(
  "disconnected host shows retry and emits retryHost",
  async ({ page }) => {
    const multiHostData = {
      ...testWorkspaceData,
      hosts: [
        primaryHost,
        {
          key: "remote",
          label: "Build Server",
          connectionState: "disconnected" as const,
          projects: [],
          sessions: [],
          resources: null,
        },
      ],
    } satisfies WorkspaceData;

    await injectWithCallback(page, multiHostData);
    await page.goto("/workspaces");

    // Host switcher should be visible with two hosts
    const remoteBtns = page
      .locator(".host-btn")
      .filter({ hasText: "Build Server" });
    await expect(remoteBtns).toBeVisible();

    // Disconnected host should have a Retry button
    const retryBtn = remoteBtns.locator(".retry-btn");
    await expect(retryBtn).toBeVisible();
    await retryBtn.click();

    const command = await getLastCommand(page);
    expect(command).toBeTruthy();
    expect(command.cmd).toBe("retryHost");
    expect(command.payload).toEqual({ hostKey: "remote" });
  },
);

test(
  "update_selection sets both host and worktree atomically",
  async ({ page }) => {
    await page.addInitScript((d) => {
      window.__middleman_config = { workspace: d };
    }, testWorkspaceData);
    await page.goto("/workspaces");

    await page.evaluate(() => {
      window.__middleman_update_selection?.({
        hostKey: "local",
        worktreeKey: "wt-2",
      });
    });

    // wt-2 (renders as "Add auth middleware") should now be selected
    const selectedRow = page
      .locator(".worktree-row")
      .filter({ hasText: "Add auth middleware" });
    await expect(selectedRow).toHaveClass(/selected/);
  },
);

test(
  "update_selection changing host clears worktree",
  async ({ page }) => {
    await page.addInitScript((d) => {
      window.__middleman_config = { workspace: d };
    }, testWorkspaceData);
    await page.goto("/workspaces");

    // Verify wt-1 starts selected
    const mainRow = page
      .locator(".worktree-row")
      .filter({ hasText: "main" });
    await expect(mainRow).toHaveClass(/selected/);

    // Change host without providing worktreeKey
    await page.evaluate(() => {
      window.__middleman_update_selection?.({
        hostKey: "other",
      });
    });

    // No worktree should be selected now
    const selectedRows = page.locator(".worktree-row.selected");
    await expect(selectedRows).toHaveCount(0);
  },
);

test(
  "update_host_state shows retry in host chip when disconnected",
  async ({ page }) => {
    await page.addInitScript((d) => {
      window.__middleman_config = { workspace: d };
    }, testWorkspaceData);
    await page.goto("/workspaces");

    // Host starts connected — chip visible, no retry button
    const chip = page.getByTestId("single-host-chip");
    await expect(chip).toBeVisible();
    await expect(chip.locator(".retry-btn")).toHaveCount(0);

    // Patch host to disconnected
    await page.evaluate(() => {
      window.__middleman_update_host_state?.(
        "local",
        { connectionState: "disconnected" },
      );
    });

    // Chip should now show retry button
    await expect(chip.locator(".retry-btn")).toBeVisible();
  },
);

test(
  "set_repo_filter bridge sets and clears filter",
  async ({ page }) => {
    await page.goto("/pulls");

    await page.evaluate(() => {
      window.__middleman_set_repo_filter?.({
        owner: "acme",
        name: "backend",
      });
    });

    const repo = await page.evaluate(() => {
      return localStorage.getItem("middleman-filter-repo");
    });
    expect(repo).toBe("acme/backend");

    await page.evaluate(() => {
      window.__middleman_set_repo_filter?.(null);
    });

    const cleared = await page.evaluate(() => {
      return localStorage.getItem("middleman-filter-repo");
    });
    expect(cleared).toBeNull();
  },
);

test(
  "embed.activePlatformHost is readable from config",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { activePlatformHost: "github.com" },
      };
    });
    await page.goto("/workspaces");

    const host = await page.evaluate(
      () =>
        window.__middleman_config?.embed?.activePlatformHost,
    );
    expect(host).toBe("github.com");
  },
);

test(
  "embed.initialRoute navigates on mount",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { initialRoute: "/workspaces" },
      };
    });

    await page.goto("/");

    await expect(page).toHaveURL(/\/workspaces/);
    await expect(
      page.getByText("No workspace data available"),
    ).toBeVisible();
  },
);

test(
  "WorkspacesView sidebar-only mode still works",
  async ({ page }) => {
    await page.addInitScript((data) => {
      window.__middleman_config = {
        workspace: data,
        onWorkspaceCommand: () => ({ ok: true }),
        embed: { sidebarWidth: 300 },
      };
    }, testWorkspaceData);

    await page.goto("/workspaces");

    await expect(
      page.locator(".worktree-row"),
    ).toHaveCount(2);
    // No resize handle in sidebar-only mode
    await expect(
      page.locator(".resize-handle"),
    ).toHaveCount(0);
  },
);

test(
  "onWorkspaceCommand returning CommandResult works without error",
  async ({ page }) => {
    await page.addInitScript((data) => {
      window.__middleman_config = {
        workspace: data,
        onWorkspaceCommand: (
          cmd: string,
          // eslint-disable-next-line @typescript-eslint/no-unused-vars
          payload: Record<string, unknown>,
        ) => {
          if (cmd === "selectWorktree") {
            return { ok: true };
          }
          return { ok: false, message: "unknown" };
        },
      };
    }, testWorkspaceData);

    await page.goto("/workspaces");

    const row = page
      .locator(".worktree-row")
      .filter({ hasText: "Add auth middleware" });
    await expect(row).toBeVisible();
    await row.click();

    // Verify no console errors from the command handler
    const errors: string[] = [];
    page.on("pageerror", (err) => errors.push(err.message));

    // Click another row to trigger a second command
    const mainRow = page
      .locator(".worktree-row")
      .filter({ hasText: "main" });
    await mainRow.click();

    expect(errors).toHaveLength(0);
  },
);

test(
  "empty hosts renders without error",
  async ({ page }) => {
    const emptyData: WorkspaceData = {
      hosts: [],
      selectedWorktreeKey: null,
      selectedHostKey: null,
    };
    await page.addInitScript((data) => {
      window.__middleman_config = {
        workspace: data,
        onWorkspaceCommand: () => ({ ok: true }),
      };
    }, emptyData);

    const errors: string[] = [];
    page.on("pageerror", (err) => errors.push(err.message));

    await page.goto("/workspaces");

    // Should not crash — renders the sidebar container
    // with no projects, sessions, or host switcher
    await expect(
      page.locator(".workspace-sidebar"),
    ).toBeVisible();
    expect(errors).toHaveLength(0);
  },
);

test(
  "hidden worktrees emit commands with parent keys",
  async ({ page }) => {
    const dataWithHidden = {
      ...testWorkspaceData,
      hosts: [{
        ...primaryHost,
        projects: [{
          ...primaryProject,
          worktrees: [
            ...primaryProject.worktrees,
            {
              key: "wt-hidden",
              name: "hidden-branch",
              branch: "hidden/branch",
              isPrimary: false,
              isHidden: true,
              isStale: false,
              sessionBackend: null,
              linkedPR: null,
              activity: {
                state: "idle" as const,
                lastOutputAt: null,
              },
              diff: null,
            },
          ],
        }],
      }],
    } satisfies WorkspaceData;
    await page.addInitScript((data) => {
      window.__middleman_config = {
        workspace: data,
        onWorkspaceCommand: (
          _cmd: string,
          payload: Record<string, unknown>,
        ) => {
          // eslint-disable-next-line @typescript-eslint/no-explicit-any -- test-only
          (window as Record<string, any>).__lastPayload =
            payload;
          return { ok: true };
        },
      };
    }, dataWithHidden);

    await page.goto("/workspaces");

    // Expand hidden worktrees
    await page
      .getByRole("button", { name: /show 1 hidden/i })
      .click();

    // Click the hidden worktree row
    const hiddenRow = page
      .locator(".worktree-row")
      .filter({ hasText: "hidden-branch" });
    await hiddenRow.click();

    const payload = await page.evaluate(
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- test-only
      () => (window as Record<string, any>).__lastPayload,
    );
    expect(payload).toBeTruthy();
    expect(payload.hostKey).toBe("local");
    expect(payload.projectKey).toBe("proj-1");
    expect(payload.worktreeKey).toBe("wt-hidden");
  },
);

test(
  "long-hover over a worktree row emits requestHoverCard",
  async ({ page }) => {
    await page.addInitScript((data) => {
      window.__middleman_config = {
        workspace: data,
        embed: { hoverCardsEnabled: true },
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
    }, testWorkspaceData);
    await page.goto("/workspaces");
    const row = page.locator(".worktree-row").first();
    await row.hover();
    await page.waitForTimeout(600);
    const command = await page.evaluate(
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- test-only window property
      () => (window as Record<string, any>).__last_workspace_command,
    );
    expect(command.cmd).toBe("requestHoverCard");
    expect(command.payload.worktreeKey).toBeDefined();
    expect(command.payload.anchorRect).toBeDefined();
  },
);

test(
  "hover card not emitted when hoverCardsEnabled is false",
  async ({ page }) => {
    await page.addInitScript((data) => {
      window.__middleman_config = {
        workspace: data,
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
    }, testWorkspaceData);
    await page.goto("/workspaces");
    const row = page.locator(".worktree-row").first();
    await row.hover();
    await page.waitForTimeout(600);
    const command = await page.evaluate(
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- test-only window property
      () => (window as Record<string, any>).__last_workspace_command,
    );
    // No command should have been emitted
    expect(command).toBeFalsy();
  },
);
