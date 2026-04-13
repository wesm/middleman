import { expect, test } from "@playwright/test";

test("budget bars render with seeded rate-limit data", async ({ page }) => {
  await page.goto("/pulls");

  const bars = page.locator(".budget-bars");
  await expect(bars).toBeVisible();
  await expect(bars.getByText("REST")).toBeVisible();
});

test("popover shows per-host breakdown from seeded data", async ({ page }) => {
  await page.goto("/pulls");

  await page.locator(".budget-bars").click();

  // Popover exposes itself as a dialog with the expected accessible name.
  const popover = page.getByRole("dialog", { name: "API Budget" });
  await expect(popover).toBeVisible();
  await expect(popover.getByText("req", { exact: true })).toBeVisible();
  await expect(popover.getByText("pts", { exact: true })).toBeVisible(); // GQL data
  await expect(popover.getByText(/75/)).toBeVisible(); // budget_spent
});

test("popover opens via keyboard (Enter)", async ({ page }) => {
  await page.goto("/pulls");

  const bars = page.locator(".budget-bars");
  await bars.focus();
  await page.keyboard.press("Enter");

  await expect(page.locator(".budget-popover")).toBeVisible();
});

test("popover opens via keyboard (Space)", async ({ page }) => {
  await page.goto("/pulls");

  const bars = page.locator(".budget-bars");
  await bars.focus();
  await page.keyboard.press("Space");

  await expect(page.locator(".budget-popover")).toBeVisible();
});
