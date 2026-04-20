import { expect, test } from "@playwright/test";

import { mockApi } from "./support/mockApi";

test.beforeEach(async ({ page }) => {
  await mockApi(page);
});

test("workspaces route renders the terminal workspace list shell", async ({ page }) => {
  await page.goto("/workspaces");
  await expect(
    page.getByText("Select a workspace from the sidebar"),
  ).toBeVisible();
});

test("AppHeader workspaces tab navigates to /workspaces", async ({ page }) => {
  await page.goto("/pulls");
  await page
    .getByRole("button", { name: "Workspaces" })
    .click();
  await expect(page).toHaveURL(/\/workspaces$/);
});

test(
  "repo selector renders icon and still filters repos",
  async ({ page }) => {
    await page.goto("/pulls");

    const selector = page.getByTitle(
      "Select repository",
    );
    await expect(selector).toBeVisible();
    await expect(selector.locator("svg")).toBeVisible();

    await selector.click();

    const input = page.getByLabel("Filter repos");
    await expect(input).toBeVisible();
    await input.fill("widg");

    const option = page.getByRole("option", {
      name: "acme/widgets",
    });
    await expect(option).toBeVisible();
    await option.click();

    await expect(selector).toContainText("acme/widgets");
    await expect(selector.locator("svg")).toBeVisible();
    await expect(
      page.getByText("Add browser regression coverage"),
    ).toBeVisible();
  },
);

test("hideHeader suppresses AppHeader on the workspaces page", async ({ page }) => {
  await page.addInitScript(() => {
    window.__middleman_config = {
      embed: { hideHeader: true },
    };
  });

  await page.goto("/workspaces");
  await expect(
    page.locator("header.app-header"),
  ).toHaveCount(0);
});

test("navigateToRoute bridge method works", async ({ page }) => {
  await page.goto("/pulls");
  await page.evaluate(() => {
    window.__middleman_navigate_to_route?.("/workspaces");
  });
  await expect(page).toHaveURL(/\/workspaces/);
});
