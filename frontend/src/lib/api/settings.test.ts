import { beforeEach, describe, expect, it, vi } from "vitest";
import { bulkAddRepos, previewRepos, removeRepo } from "./settings.js";

describe("settings api", () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: vi.fn().mockResolvedValue({ repos: [] }),
      text: vi.fn().mockResolvedValue(""),
    }) as unknown as typeof fetch;
  });

  it("encodes repo names for delete requests", async () => {
    await removeRepo("acme", "widgets-?");

    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/repos/acme/widgets-%3F",
      { method: "DELETE", headers: { "Content-Type": "application/json" } },
    );
  });

  it("posts preview requests", async () => {
    await previewRepos("acme", "widget-*");

    expect(fetch).toHaveBeenCalledWith("/api/v1/repos/preview", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ owner: "acme", pattern: "widget-*" }),
    });
  });

  it("posts bulk add requests", async () => {
    await bulkAddRepos([{ owner: "acme", name: "api" }]);

    expect(fetch).toHaveBeenCalledWith("/api/v1/repos/bulk", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ repos: [{ owner: "acme", name: "api" }] }),
    });
  });

  it("uses json error envelope when present", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false,
      status: 400,
      clone: vi.fn().mockReturnValue({ text: vi.fn().mockResolvedValue("") }),
      json: vi.fn().mockResolvedValue({ error: "invalid glob pattern" }),
      text: vi.fn(),
    });

    await expect(previewRepos("acme", "[")).rejects.toThrow("invalid glob pattern");
  });
});
