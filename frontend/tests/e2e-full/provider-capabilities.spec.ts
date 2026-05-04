import { expect, test } from "@playwright/test";

test.describe("provider capabilities", () => {
  test("GitLab issue detail hides timeline edit controls when comments are read-only", async ({ page }) => {
    await page.goto(
      "/issues/group/project/11?platform_host=gitlab.example.com",
    );

    const detail = page.locator(".issue-detail");
    await expect(detail).toBeVisible();
    await expect(
      detail.getByText("GitLab read-only issue"),
    ).toBeVisible();
    await expect(
      detail.getByText("GitLab read-only timeline comment"),
    ).toBeVisible();
    await expect(
      detail.getByRole("button", { name: "Edit comment" }),
    ).toHaveCount(0);
    await expect(
      detail.getByRole("button", { name: "Copy comment" }),
    ).toBeVisible();
  });
});
