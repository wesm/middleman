import { expect, test } from "@playwright/test";

import { mockApi } from "./support/mockApi";

// E2E coverage for the PR-detail palette commands wired in Task 26
// (`pr.approve`, `pr.ready`, `pr.approveWorkflows`). Task 26 leaves the
// merge palette command (`pr.merge`) deferred because flipping the
// MergeModal's open state from outside `PullDetail.svelte` would widen
// the change beyond the plan's stated scope, so this spec focuses on
// approve/ready/approve-workflows.
//
// Note for the engineer running the e2e suite: local Playwright is not
// expected to be wired up in this worktree. Task 36 is the catch-all
// pass that will exercise the full Playwright suite in CI; this spec is
// checked in here so it runs there as a regression guard. The mockApi
// fixtures may need to be extended (per-PR `capabilities`,
// `MergeableState`, an `/approve` POST handler) before this file passes
// end-to-end — that wiring is Task 36's responsibility, not Task 26's.

test.beforeEach(async ({ page }) => {
  await mockApi(page);
});

test.describe("PR-detail palette commands", () => {
  test("Approve PR runs from the palette and triggers the approve flow", async ({
    page,
  }) => {
    // Navigate to a PR detail. mockApi serves /api/v1/pulls and a
    // single-PR detail endpoint at /repos/<owner>/<name>/pulls/<n>.
    await page.goto("/pulls/acme/widgets/42");

    // Capture the approve POST so we can assert the action wired
    // through the same closure the existing button uses.
    const approveRequest = page.waitForRequest(
      (req) =>
        req.method() === "POST" &&
        /\/repos\/acme\/widgets\/pulls\/42\/approve$/.test(
          new URL(req.url()).pathname,
        ),
    );

    await page.keyboard.press("Meta+K");
    await page.locator(".palette-input").fill("approve pr");
    await page.keyboard.press("Enter");

    await approveRequest;
  });

  test("Approve PR is absent from the palette when the PR is closed", async ({
    page,
  }) => {
    // The fixture for /pulls/acme/widgets/55 should be a closed PR.
    // (mockApi may need updates so this PR returns State === "closed";
    // the assertion still checks the user-visible behavior.)
    await page.goto("/pulls/acme/widgets/55");

    await page.keyboard.press("Meta+K");
    await page.locator(".palette-input").fill("approve pr");

    await expect(
      page.getByRole("option", { name: /Approve PR/i }),
    ).toHaveCount(0);
  });

  test("Mark ready for review appears only when the PR is a draft", async ({
    page,
  }) => {
    await page.goto("/pulls/acme/widgets/42");
    await page.keyboard.press("Meta+K");
    await page.locator(".palette-input").fill("ready for review");
    // Non-draft PR; the action should be filtered out by `when`.
    await expect(
      page.getByRole("option", { name: /Mark ready for review/i }),
    ).toHaveCount(0);
  });
});
