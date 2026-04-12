import { expect, test } from "@playwright/test";

async function fetchPullDetail(page: import("@playwright/test").Page, number: number) {
  return page.evaluate(async (prNumber) => {
    const response = await fetch(`/api/v1/repos/acme/widgets/pulls/${prNumber}`);
    return response.json();
  }, number);
}

async function fetchIssueDetail(page: import("@playwright/test").Page, number: number) {
  return page.evaluate(async (issueNumber) => {
    const response = await fetch(`/api/v1/repos/acme/widgets/issues/${issueNumber}`);
    return response.json();
  }, number);
}

function expectUTCString(value: string | null): void {
  expect(value).toBeTruthy();
  expect(value).toMatch(/Z$/);
}

test.describe("UTC maintainer flows", () => {
  test("closing and reopening a pull request keeps API timestamps canonical UTC", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/6");
    await expect(page.locator(".btn--close")).toBeVisible();

    await page.locator(".btn--close").click();
    await expect(page.locator(".btn--reopen")).toBeVisible();

    const closed = await fetchPullDetail(page, 6);
    expect(closed.merge_request.State).toBe("closed");
    expectUTCString(closed.merge_request.ClosedAt);

    await page.locator(".btn--reopen").click();
    await expect(page.locator(".btn--close")).toBeVisible();

    const reopened = await fetchPullDetail(page, 6);
    expect(reopened.merge_request.State).toBe("open");
    expect(reopened.merge_request.ClosedAt).toBeNull();
  });

  test("merging a pull request stores UTC timestamps and updates the detail view", async ({ page }) => {
    await page.goto("/pulls/acme/widgets/7");
    await expect(page.locator(".btn--merge")).toBeVisible();

    await page.locator(".btn--merge").click();
    await expect(page.locator(".modal-title")).toHaveText("Merge Pull Request");
    await page.locator(".btn--primary.btn--green").click();

    await expect(page.locator(".chip.chip--purple")).toHaveText("Merged");

    const merged = await fetchPullDetail(page, 7);
    expect(merged.merge_request.State).toBe("merged");
    expectUTCString(merged.merge_request.ClosedAt);
    expectUTCString(merged.merge_request.MergedAt);
  });

  test("closing and reopening an issue keeps API timestamps canonical UTC", async ({ page }) => {
    await page.goto("/issues/acme/widgets/11");
    await expect(page.locator(".btn--close")).toBeVisible();

    await page.locator(".btn--close").click();
    await expect(page.locator(".btn--reopen")).toBeVisible();

    const closed = await fetchIssueDetail(page, 11);
    expect(closed.issue.State).toBe("closed");
    expectUTCString(closed.issue.ClosedAt);

    await page.locator(".btn--reopen").click();
    await expect(page.locator(".btn--close")).toBeVisible();

    const reopened = await fetchIssueDetail(page, 11);
    expect(reopened.issue.State).toBe("open");
    expect(reopened.issue.ClosedAt).toBeNull();
  });
});
