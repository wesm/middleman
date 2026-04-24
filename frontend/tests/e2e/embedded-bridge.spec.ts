import { expect, test } from "@playwright/test";

import { mockApi } from "./support/mockApi";

test.beforeEach(async ({ page }) => {
  await mockApi(page);
});

test("embedded bridge keeps workspace update methods active on pulls route", async ({ page }) => {
  await page.addInitScript(() => {
    window.__middleman_config = {
      workspace: {
        selectedHostKey: null,
        selectedWorktreeKey: null,
        hosts: [
          {
            key: "host-1",
            label: "Host 1",
            connectionState: "connected",
            projects: [],
            sessions: [],
            resources: null,
          },
        ],
      },
    };
  });

  await page.goto("/pulls");
  await expect(page.locator(".pull-item").first()).toBeVisible();

  const workspace = await page.evaluate(() => {
    window.__middleman_update_selection?.({
      hostKey: "host-1",
      worktreeKey: "wt-1",
    });
    window.__middleman_update_host_state?.("host-1", {
      connectionState: "error",
    });
    window.__middleman_update_workspace?.({
      selectedHostKey: "host-2",
      selectedWorktreeKey: null,
      hosts: [],
    });

    return window.__middleman_config?.workspace;
  });

  expect(workspace).toEqual({
    selectedHostKey: "host-2",
    selectedWorktreeKey: null,
    hosts: [],
  });
});
