import { expect, test } from "@playwright/test";

import { mockApi } from "./support/mockApi";

test.beforeEach(async ({ page }) => {
  await mockApi(page);
});

test("renders mocked frontend data", async ({ page }) => {
  await page.goto("/pulls");

  await expect(page.getByText("Add browser regression coverage")).toBeVisible();
  await expect(page.getByText("acme/widgets")).toBeVisible();
  await expect(page.getByText("1 PRs")).toBeVisible();
  await expect(page.getByText("1 repos")).toBeVisible();

  await page.getByRole("button", { name: "Issues" }).click();

  await expect(page.getByText("Theme toggle does not stick")).toBeVisible();
  await expect(page.getByText("1 issues")).toBeVisible();
});

test("toggles dark mode from the header control", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto("/");

  const root = page.locator("html");
  const button = page.getByTitle("Toggle theme");

  await expect(root).not.toHaveClass(/dark/);

  await button.click();
  await expect(root).toHaveClass(/dark/);

  await button.click();
  await expect(root).not.toHaveClass(/dark/);
});
