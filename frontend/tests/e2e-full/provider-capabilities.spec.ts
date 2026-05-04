import { expect, test } from "@playwright/test";
import {
  startIsolatedE2EServerWithOptions,
  type IsolatedE2EServer,
} from "./support/e2eServer";

test.describe("provider capabilities", () => {
  test("GitLab issue detail hides timeline edit controls when comments are read-only", async ({ page }) => {
    let isolatedServer: IsolatedE2EServer | null = null;
    try {
      isolatedServer = await startIsolatedE2EServerWithOptions({
        gitLabReadOnlyFixture: true,
      });
      await page.goto(
        `${isolatedServer.info.base_url}/issues/group/project/11?platform_host=gitlab.example.com`,
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
    } finally {
      await isolatedServer?.stop();
    }
  });
});
