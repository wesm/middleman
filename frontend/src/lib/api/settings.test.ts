import { beforeEach, describe, expect, it, vi } from "vitest";

describe("settings api", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        Response.json({ repos: [], owner: "acme", pattern: "widget-*" }),
      ),
    );
  });

  it("encodes repo names for delete requests", async () => {
    const { removeRepo } = await import("./settings.js");

    await removeRepo("acme", "widgets-?");

    const request = vi.mocked(fetch).mock.calls[0]?.[0];
    expect(request).toBeInstanceOf(Request);
    expect(new URL((request as Request).url).pathname).toBe(
      "/api/v1/repos/acme/widgets-%3F",
    );
    expect((request as Request).method).toBe("DELETE");
  });

  it("posts preview requests", async () => {
    const { previewRepos } = await import("./settings.js");

    await previewRepos("acme", "widget-*");

    const request = vi.mocked(fetch).mock.calls[0]?.[0];
    expect(request).toBeInstanceOf(Request);
    expect(new URL((request as Request).url).pathname).toBe(
      "/api/v1/repos/preview",
    );
    expect((request as Request).method).toBe("POST");
    await expect((request as Request).clone().json()).resolves.toEqual({
      owner: "acme",
      pattern: "widget-*",
    });
  });

  it("posts bulk add requests", async () => {
    const { bulkAddRepos } = await import("./settings.js");

    await bulkAddRepos([{ owner: "acme", name: "api" }]);

    const request = vi.mocked(fetch).mock.calls[0]?.[0];
    expect(request).toBeInstanceOf(Request);
    expect(new URL((request as Request).url).pathname).toBe(
      "/api/v1/repos/bulk",
    );
    expect((request as Request).method).toBe("POST");
    await expect((request as Request).clone().json()).resolves.toEqual({
      repos: [{ owner: "acme", name: "api" }],
    });
  });

  it("uses json error envelope when present", async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      Response.json(
        { detail: "invalid glob pattern" },
        {
          status: 400,
          headers: { "Content-Type": "application/problem+json" },
        },
      ),
    );
    const { previewRepos } = await import("./settings.js");

    await expect(previewRepos("acme", "[")).rejects.toThrow("invalid glob pattern");
  });
});
