import { afterEach, describe, expect, it, vi } from "vitest";

async function importRouterAt(path: string) {
  vi.resetModules();
  window.history.replaceState(null, "", path);
  return import("./router.svelte.js");
}

describe("router initialization", () => {
  afterEach(() => {
    window.history.replaceState(null, "", "/");
    vi.resetModules();
  });

  it("preserves issue platform_host query state on initial load", async () => {
    const { getRoute } = await importRouterAt(
      "/issues/acme/widget/7?platform_host=ghe.example.com",
    );

    expect(getRoute()).toEqual({
      page: "issues",
      selected: {
        owner: "acme",
        name: "widget",
        number: 7,
        platformHost: "ghe.example.com",
      },
    });
  });

  it("preserves issue platform_host query state on popstate", async () => {
    const { getRoute } = await importRouterAt("/issues/acme/widget/7");

    window.history.pushState(
      null,
      "",
      "/issues/acme/widget/7?platform_host=ghe.example.com",
    );
    window.dispatchEvent(new PopStateEvent("popstate"));

    expect(getRoute()).toEqual({
      page: "issues",
      selected: {
        owner: "acme",
        name: "widget",
        number: 7,
        platformHost: "ghe.example.com",
      },
    });
  });
});
