import { expect, test } from "@playwright/test";

import { mockApi } from "./support/mockApi";

test.beforeEach(async ({ page }) => {
  await mockApi(page);
});

test("design system page renders chip matrix with shared styles", async ({ page }) => {
  await page.goto("/design-system");

  await expect(
    page.getByRole("heading", { name: "Design system" }),
  ).toBeVisible();

  const smGreenChip = page.locator('[data-size="sm"] .chip--green', {
    hasText: "Green",
  }).first();
  const mdGreenChip = page.locator('[data-size="md"] .chip--green', {
    hasText: "Green",
  }).first();
  const mutedChip = page.locator(".chip--muted", {
    hasText: "Muted",
  }).first();
  const plainCaseChip = page.getByText("plain case", { exact: true }).first();
  const interactiveChip = page.getByRole("button", {
    name: "Interactive",
  }).first();

  await expect(smGreenChip).toBeVisible();
  await expect(mdGreenChip).toBeVisible();
  await expect(mutedChip).toBeVisible();
  await expect(plainCaseChip).toBeVisible();
  await expect(interactiveChip).toBeVisible();

  const styles = await Promise.all([
    smGreenChip.evaluate((node) => {
      const styles = getComputedStyle(node);
      return {
        minHeight: styles.minHeight,
      };
    }),
    mdGreenChip.evaluate((node) => {
      const styles = getComputedStyle(node);
      return {
        minHeight: styles.minHeight,
        backgroundColor: styles.backgroundColor,
        textTransform: styles.textTransform,
      };
    }),
    mutedChip.evaluate((node) => {
      const styles = getComputedStyle(node);
      return {
        backgroundColor: styles.backgroundColor,
      };
    }),
    plainCaseChip.evaluate((node) => {
      const styles = getComputedStyle(node);
      return {
        textTransform: styles.textTransform,
        letterSpacing: styles.letterSpacing,
      };
    }),
    interactiveChip.evaluate((node) => {
      const styles = getComputedStyle(node);
      return {
        cursor: styles.cursor,
      };
    }),
  ]);

  expect(styles[0].minHeight).toBe("20px");
  expect(styles[1].minHeight).toBe("22px");
  expect(styles[1].backgroundColor).not.toBe("rgba(0, 0, 0, 0)");
  expect(styles[1].textTransform).toBe("uppercase");
  expect(styles[2].backgroundColor).not.toBe("rgba(0, 0, 0, 0)");
  expect(styles[3].textTransform).toBe("none");
  expect(styles[3].letterSpacing).toBe("normal");
  expect(styles[4].cursor).toBe("pointer");
});
