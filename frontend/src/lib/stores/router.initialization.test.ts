import { afterEach, describe, expect, it, vi } from "vitest";

const issueRoute =
  "/host/ghe.example.com/issues/github/acme/widget/7";

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

  it("preserves provider issue route state on initial load", async () => {
    const { getRoute } = await importRouterAt(issueRoute);

    expect(getRoute()).toEqual({
      page: "issues",
      selected: {
        provider: "github",
        owner: "acme",
        name: "widget",
        repoPath: "acme/widget",
        number: 7,
        platformHost: "ghe.example.com",
      },
    });
  });

  it("preserves provider issue route state on popstate", async () => {
    const { getRoute } = await importRouterAt("/issues");

    window.history.pushState(null, "", issueRoute);
    window.dispatchEvent(new PopStateEvent("popstate"));

    expect(getRoute()).toEqual({
      page: "issues",
      selected: {
        provider: "github",
        owner: "acme",
        name: "widget",
        repoPath: "acme/widget",
        number: 7,
        platformHost: "ghe.example.com",
      },
    });
  });
});
