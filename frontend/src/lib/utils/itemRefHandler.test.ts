import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  post: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("../api/runtime.js", () => ({
  client: {
    POST: mocks.post,
  },
}));

vi.mock("../stores/router.svelte.js", () => ({
  buildItemRoute: (
    type: "pr" | "issue",
    owner: string,
    name: string,
    number: number,
    platformHost?: string,
  ) => {
    const hostQuery = platformHost ? `?platform_host=${platformHost}` : "";
    return `/${type}/${owner}/${name}/${number}${hostQuery}`;
  },
  navigate: mocks.navigate,
}));

vi.mock("../stores/flash.svelte.js", () => ({
  showFlash: vi.fn(),
}));

import { initItemRefHandler } from "./itemRefHandler.js";

describe("itemRefHandler", () => {
  let cleanupHandler: (() => void) | undefined;

  beforeEach(() => {
    document.body.innerHTML = "";
    mocks.post.mockReset();
    mocks.navigate.mockReset();
  });

  afterEach(() => {
    cleanupHandler?.();
    cleanupHandler = undefined;
    document.body.innerHTML = "";
  });

  it("preserves platformHost when resolved item references are PRs", async () => {
    mocks.post.mockResolvedValue({
      data: {
        item_type: "pr",
        number: 42,
        repo_tracked: true,
      },
    });
    cleanupHandler = initItemRefHandler();
    document.body.innerHTML = `
      <a class="item-ref"
        href="/pulls/acme/widget/42"
        data-owner="acme"
        data-name="widget"
        data-number="42"
        data-platform-host="ghe.example.com">#42</a>
    `;

    document.querySelector<HTMLAnchorElement>(".item-ref")?.click();
    await Promise.resolve();

    expect(mocks.post).toHaveBeenCalledWith(
      "/repos/{owner}/{name}/items/{number}/resolve",
      {
        params: {
          path: { owner: "acme", name: "widget", number: 42 },
          query: { platform_host: "ghe.example.com" },
        },
      },
    );
    expect(mocks.navigate).toHaveBeenCalledWith(
      "/pr/acme/widget/42?platform_host=ghe.example.com",
    );
  });
});
