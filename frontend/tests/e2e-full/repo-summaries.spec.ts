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

    await widgetsCard.getByRole("button", { name: "New issue" }).click();
    await widgetsCard.getByPlaceholder("Issue title").fill("Repo overview follow-up");
    await widgetsCard
      .getByPlaceholder(
        "Describe the problem, context, or follow-up work",
      )
      .fill("Add additional filters to the repository dashboard.");
    await widgetsCard.getByRole("button", { name: "Create issue" }).click();

    await expect(page).toHaveURL(/\/issues\/acme\/widgets\/\d+$/);
    await page.locator(".issue-detail").waitFor({
      state: "visible",
      timeout: 10_000,
    });
    await expect(page.locator(".issue-detail")).toContainText(
      "Repo overview follow-up",
    );
  });
});
