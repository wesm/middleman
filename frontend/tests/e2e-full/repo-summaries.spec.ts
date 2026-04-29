import { expect, test } from "@playwright/test";

test.describe("repository summaries", () => {
  test("remembers filters after tab changes", async ({ page }) => {
    await page.goto("/repos");

    await page.getByPlaceholder("Filter repositories").fill("acme");
    await page.getByRole("button", { name: "Has issues" }).click();
    await page.locator(".repo-page__sort-dropdown")
      .getByRole("button", { name: "Name" })
      .click();
    await page.locator(".filter-dropdown")
      .getByRole("button", { name: "Open issues" })
      .click();

    const repoCards = page.locator(".repo-card");
    await expect(repoCards).toHaveCount(2);
    await expect(
      repoCards.nth(0).getByRole("button", { name: /acme\s*\/\s*widgets/ }),
    ).toBeVisible();
    await expect(
      repoCards.nth(1).getByRole("button", { name: /acme\s*\/\s*tools/ }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: /acme\s*\/\s*archived/ }),
    ).toHaveCount(0);

    await page.getByRole("button", { name: "PRs", exact: true }).click();
    await expect(page).toHaveURL(/\/pulls$/);

    await page.getByRole("button", { name: "Repos", exact: true }).click();
    await expect(page).toHaveURL(/\/repos$/);
    await expect(
      page.getByPlaceholder("Filter repositories"),
    ).toHaveValue("acme");
    await expect(
      page.getByRole("button", { name: "Has issues" }),
    ).toHaveClass(/repo-page__filter--active/);
    await expect(
      page.locator(".repo-page__sort-dropdown")
        .getByRole("button", { name: "Open issues" }),
    ).toBeVisible();

    await expect(repoCards).toHaveCount(2);
    await expect(
      repoCards.nth(0).getByRole("button", { name: /acme\s*\/\s*widgets/ }),
    ).toBeVisible();
    await expect(
      repoCards.nth(1).getByRole("button", { name: /acme\s*\/\s*tools/ }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: /acme\s*\/\s*archived/ }),
    ).toHaveCount(0);
  });

  test("shows repo stats and can create an issue", async ({ page }) => {
    await page.goto("/repos");

    const widgetsCard = page
      .locator(".repo-card")
      .filter({
        has: page.getByRole("button", {
          name: /acme\s*\/\s*widgets/,
        }),
      })
      .first();

    await widgetsCard.waitFor({ state: "visible", timeout: 10_000 });
    await expect(page.getByText("Total repos")).toBeVisible();
    await expect(widgetsCard).toContainText("Open PRs");
    await expect(widgetsCard).toContainText("Recent open issues");

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

    await expect(page).toHaveURL(
      /\/issues\/acme\/widgets\/\d+\?platform_host=github\.com$/,
    );
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
