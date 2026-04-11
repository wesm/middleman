import { expect, test } from "@playwright/test";

test.describe("comment editor autocomplete", () => {
  test("PR comment editor accepts @ mention and submits end-to-end", async ({ page }) => {
    await page.goto("/pulls");
    await page.locator(".pull-item").first().waitFor({ state: "visible", timeout: 10_000 });
    await page.locator(".pull-item").first().click();
    await page.locator(".pull-detail").waitFor({ state: "visible", timeout: 10_000 });

    const detail = page.locator(".pull-detail");
    const editor = detail.locator(".comment-editor-input").first();
    await editor.click();
    await page.keyboard.type("<script>alert('x')</script> @a");

    const firstSuggestion = page.locator(".comment-editor-option").first();
    await expect(firstSuggestion).toBeVisible({ timeout: 10_000 });
    await editor.press("Enter");
    await expect(editor).toContainText("@alice");

    const submit = detail.locator(".submit-btn");
    await expect(submit).toBeEnabled();
    await submit.click();

    await expect(detail.locator(".event-body").filter({ hasText: /@alice/ }).first()).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("Select a PR")).toHaveCount(0);
  });

  test("issue comment editor accepts # reference and submits end-to-end", async ({ page }) => {
    await page.goto("/issues");
    await page.locator(".issue-item").first().waitFor({ state: "visible", timeout: 10_000 });
    await page.locator(".issue-item").first().click();
    await page.locator(".issue-detail").waitFor({ state: "visible", timeout: 10_000 });

    const detail = page.locator(".issue-detail");
    const editor = detail.locator(".comment-editor-input").first();
    await editor.fill("See #1");

    await expect(page.locator(".comment-editor-option").first()).toBeVisible({ timeout: 10_000 });
    await editor.press("Enter");

    const submit = detail.locator(".submit-btn");
    await expect(submit).toBeEnabled();
    await submit.click();

    await expect(detail.locator(".event-body").filter({ hasText: /See #1/ }).first()).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("Select a PR")).toHaveCount(0);
  });
});
