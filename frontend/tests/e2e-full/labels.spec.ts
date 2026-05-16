import { expect, test } from "@playwright/test";
import { startIsolatedE2EServer, type IsolatedE2EServer } from "./support/e2eServer";

test.describe("label editing", () => {
  async function openLabelsFromPalette(page: import("@playwright/test").Page): Promise<void> {
    await page.keyboard.press(process.platform === "darwin" ? "Meta+K" : "Control+K");
    await expect(page.getByRole("dialog", { name: "Command palette" })).toBeVisible();
    await page.locator(".palette-input").fill("Edit labels");
    await page.getByRole("button", { name: /Edit labels/ }).click();
    await expect(page.getByRole("dialog", { name: "Edit labels" })).toBeVisible();
  }

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

  test("command palette opens pull and issue label editors", async ({ page }) => {
    let isolatedServer: IsolatedE2EServer | null = null;
    try {
      isolatedServer = await startIsolatedE2EServer();
      const baseURL = isolatedServer.info.base_url;

      await page.goto(`${baseURL}/pulls/github/acme/widgets/1`);
      await expect(page.locator(".pull-detail")).toBeVisible();
      await openLabelsFromPalette(page);
      await expect(page.getByRole("menuitemcheckbox", { name: /bug/i })).toHaveAttribute("aria-checked", "true");
      await page.getByRole("button", { name: "Close label picker" }).click();

      await page.goto(`${baseURL}/issues/github/acme/widgets/10`);
      await expect(page.locator(".issue-detail")).toBeVisible();
      await openLabelsFromPalette(page);
      await expect(page.getByRole("menuitemcheckbox", { name: /bug/i })).toHaveAttribute("aria-checked", "true");
    } finally {
      await isolatedServer?.stop();
    }
  });
});
