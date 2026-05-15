import { expect, test } from "@playwright/test";
import { startIsolatedE2EServer, type IsolatedE2EServer } from "./support/e2eServer";

test.describe("label editing", () => {
  test("pull detail edits labels from repository catalog", async ({ page }) => {
    let isolatedServer: IsolatedE2EServer | null = null;
    try {
      isolatedServer = await startIsolatedE2EServer();
      const baseURL = isolatedServer.info.base_url;

      await page.goto(`${baseURL}/pulls/github/acme/widgets/1`);
      await expect(page.locator(".pull-detail")).toBeVisible();
      await expect(page.locator(".pull-detail .label-editor-row .label-pill", { hasText: "bug" })).toBeVisible();

      await page.getByRole("button", { name: "Labels" }).click();
      await expect(page.getByRole("dialog", { name: "Edit labels" })).toBeVisible();
      await expect(page.getByRole("menuitemcheckbox", { name: /bug/i })).toHaveAttribute("aria-checked", "true");
      await expect(page.getByRole("menuitemcheckbox", { name: /triage/i })).toHaveAttribute("aria-checked", "false");

      const updateResponse = page.waitForResponse((response) =>
        response.request().method() === "PUT"
        && response.url() === `${baseURL}/api/v1/pulls/github/acme/widgets/1/labels`,
      );
      await page.getByRole("menuitemcheckbox", { name: /triage/i }).click();
      expect((await updateResponse).status()).toBe(200);

      await expect(page.getByRole("menuitemcheckbox", { name: /triage/i })).toHaveAttribute("aria-checked", "true");
      await expect(page.locator(".pull-detail .label-editor-row .label-pill", { hasText: "triage" })).toBeVisible();
    } finally {
      await isolatedServer?.stop();
    }
  });
});
