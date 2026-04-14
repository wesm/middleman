/**
 * Workspace panel demo / screenshot harness.
 *
 * Usage:
 *   bun run demo-workspace            # headless, saves PNGs
 *   bun run demo-workspace --headed   # opens browser for visual inspection
 *
 * Screenshots land in frontend/tests/demo/screenshots/
 */
import { expect, test } from "@playwright/test";

import { mockApi } from "../e2e/support/mockApi";

const SCREENSHOT_DIR = "tests/demo/screenshots";

test.beforeEach(async ({ page }) => {
  await mockApi(page);
});

test("01 - empty: no selection", async ({ page }) => {
  await page.goto("/workspaces/panel/empty/noSelection");
  await expect(
    page.getByText("Select a worktree"),
  ).toBeVisible();
  await page.screenshot({
    path: `${SCREENSHOT_DIR}/01-empty-no-selection.png`,
    fullPage: true,
  });
});

test(
  "02 - empty: no platform repo",
  async ({ page }) => {
    await page.goto(
      "/workspaces/panel/empty/noPlatformRepo",
    );
    await expect(
      page.getByText("no linked repository"),
    ).toBeVisible();
    await page.screenshot({
      path: `${SCREENSHOT_DIR}/02-empty-no-platform-repo.png`,
      fullPage: true,
    });
  },
);

test("03 - starting up (null host)", async ({ page }) => {
  await page.addInitScript(() => {
    window.__middleman_config = {
      embed: { activePlatformHost: null },
    };
  });
  await page.goto(
    "/workspaces/panel/github.com/acme/widgets",
  );
  await expect(
    page.getByText("starting up"),
  ).toBeVisible();
  await page.screenshot({
    path: `${SCREENSHOT_DIR}/03-starting-up.png`,
    fullPage: true,
  });
});

test(
  "04 - non-primary host degraded",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { activePlatformHost: "github.com" },
      };
    });
    await page.goto(
      "/workspaces/panel/example.com/wesm/other-repo",
    );
    await expect(
      page.getByTestId("non-primary-state"),
    ).toBeVisible();
    await page.screenshot({
      path: `${SCREENSHOT_DIR}/04-non-primary-host.png`,
      fullPage: true,
    });
  },
);

test("05 - PR list view", async ({ page }) => {
  await page.addInitScript(() => {
    window.__middleman_config = {
      embed: { activePlatformHost: "github.com" },
      onWorkspaceCommand: () => ({ ok: true }),
    };
  });
  await page.goto(
    "/workspaces/panel/github.com/acme/widgets",
  );
  await expect(
    page.getByText("acme/widgets"),
  ).toBeVisible();
  await expect(
    page.getByText("Add browser regression coverage"),
  ).toBeVisible();
  await page.screenshot({
    path: `${SCREENSHOT_DIR}/05-list-view.png`,
    fullPage: true,
  });
});

test(
  "06 - PR list with worktree chip hover",
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
      .filter({ hasText: "Refactor theme system" });
    await row.hover();
    await expect(row.locator(".wt-chip")).toBeVisible();
    await page.screenshot({
      path: `${SCREENSHOT_DIR}/06-list-worktree-chip.png`,
      fullPage: true,
    });
  },
);

test("07 - PR detail view", async ({ page }) => {
  await page.addInitScript(() => {
    window.__middleman_config = {
      embed: { activePlatformHost: "github.com" },
      onWorkspaceCommand: () => ({ ok: true }),
    };
  });
  await page.goto(
    "/workspaces/panel/github.com/acme/widgets/42",
  );
  await expect(
    page.getByText(
      "#42 Add browser regression coverage",
    ),
  ).toBeVisible();
  await page.screenshot({
    path: `${SCREENSHOT_DIR}/07-detail-view.png`,
    fullPage: true,
  });
});

test(
  "08 - detail pinned with Unpin button",
  async ({ page }) => {
    await page.addInitScript(() => {
      window.__middleman_config = {
        embed: { activePlatformHost: "github.com" },
        onWorkspaceCommand: () => ({ ok: true }),
      };
    });
    await page.goto(
      "/workspaces/panel/github.com/acme/widgets/42?pin=hard",
    );
    await expect(
      page.locator("button.panel-unpin-btn"),
    ).toBeVisible();
    await page.screenshot({
      path: `${SCREENSHOT_DIR}/08-detail-pinned.png`,
      fullPage: true,
    });
  },
);

test("09 - detail not found", async ({ page }) => {
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
  await page.screenshot({
    path: `${SCREENSHOT_DIR}/09-detail-not-found.png`,
    fullPage: true,
  });
});
