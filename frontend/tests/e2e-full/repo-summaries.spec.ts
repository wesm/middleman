import { expect, test } from "@playwright/test";

test.describe("repository summaries", () => {
  test("shows repo stats and can create an issue", async ({ page }) => {
    await page.goto("/repos");

    await page
      .locator(".repo-card", { hasText: "acme/widgets" })
      .first()
      .waitFor({ state: "visible", timeout: 10_000 });

    await expect(page.getByText("Tracked repos")).toBeVisible();
    await expect(
      page.locator(".repo-card", { hasText: "acme/widgets" }),
    ).toContainText("Open PRs");
    await expect(
      page.locator(".repo-card", { hasText: "acme/widgets" }),
    ).toContainText("Recent open issues");

    const widgetsCard = page
      .locator(".repo-card", { hasText: "acme/widgets" })
      .first();

    await expect(
      widgetsCard.getByRole("button", { name: "View PRs" }),
    ).toHaveCount(0);
    await expect(
      widgetsCard.getByRole("button", { name: "View issues" }),
    ).toHaveCount(0);

    await widgetsCard
      .getByRole("button", { name: /\d+\s+Open PRs/ })
      .click();
    await expect(page).toHaveURL(/\/pulls$/);

    await page.goto("/repos");
    await widgetsCard.waitFor({ state: "visible", timeout: 10_000 });
    await widgetsCard
      .getByRole("button", { name: /\d+\s+Open issues/ })
      .click();
    await expect(page).toHaveURL(/\/issues$/);

    await page.goto("/repos");
    await widgetsCard.waitFor({ state: "visible", timeout: 10_000 });
    await widgetsCard.getByRole("button", { name: "New issue" }).click();

    const dialog = page.getByRole("dialog", {
      name: "New issue in acme/widgets",
    });
    await expect(dialog).toBeVisible();
    await dialog.getByPlaceholder("Issue title").fill("Repo overview follow-up");

    const bodyEditor = dialog.getByRole("textbox", {
      name: "Describe the problem, context, or follow-up work",
    });
    await bodyEditor.click();
    await page.keyboard.type("Add additional filters @al");
    await expect(
      page.getByRole("option", { name: /@alice/ }),
    ).toBeVisible();
    await page.keyboard.press("Enter");
    await expect(bodyEditor).toContainText("@alice");

    await dialog.getByRole("button", { name: "Create issue" }).click();

    await expect(page).toHaveURL(/\/issues\/acme\/widgets\/\d+$/);
    await page.locator(".issue-detail").waitFor({
      state: "visible",
      timeout: 10_000,
    });
    await expect(page.locator(".issue-detail")).toContainText(
      "Repo overview follow-up",
    );
    await expect(
      page.locator(".issue-detail .label-pill", {
        hasText: "created-from-repos",
      }),
    ).toBeVisible();
  });
});
