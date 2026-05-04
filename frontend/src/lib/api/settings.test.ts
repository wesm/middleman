import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  bulkAddRepos,
  previewRepos,
  removeRepo,
  updateSettings,
} from "./settings.js";

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
    await removeRepo("acme", "widgets-?");

    const request = vi.mocked(fetch).mock.calls[0]?.[0];
    expect(request).toBeInstanceOf(Request);
    expect(new URL((request as Request).url).pathname).toBe(
      "/api/v1/repos/acme/widgets-%3F",
    );
    expect((request as Request).method).toBe("DELETE");
  });

  it("posts preview requests", async () => {
    await previewRepos("acme", "widget-*");

    const request = vi.mocked(fetch).mock.calls[0]?.[0];
    expect(request).toBeInstanceOf(Request);
    expect(new URL((request as Request).url).pathname).toBe(
      "/api/v1/repos/preview",
    );
    expect((request as Request).method).toBe("POST");
    await expect((request as Request).clone().json()).resolves.toEqual({
      provider: "github",
      host: "github.com",
      owner: "acme",
      pattern: "widget-*",
    });
  });

  it("posts provider-aware preview requests", async () => {
    await previewRepos("group/subgroup", "Project*", {
      provider: "gitlab",
      host: "gitlab.example.com",
    });

    const request = vi.mocked(fetch).mock.calls[0]?.[0];
    expect(request).toBeInstanceOf(Request);
    await expect((request as Request).clone().json()).resolves.toEqual({
      provider: "gitlab",
      host: "gitlab.example.com",
      owner: "group/subgroup",
      pattern: "Project*",
    });
  });

  it("posts bulk add requests", async () => {
    await bulkAddRepos([{ owner: "acme", name: "api" }]);

    const request = vi.mocked(fetch).mock.calls[0]?.[0];
    expect(request).toBeInstanceOf(Request);
    expect(new URL((request as Request).url).pathname).toBe(
      "/api/v1/repos/bulk",
    );
    expect((request as Request).method).toBe("POST");
    await expect((request as Request).clone().json()).resolves.toEqual({
      repos: [{ provider: "github", host: "github.com", owner: "acme", name: "api" }],
    });
  });

  it("posts provider-aware bulk add requests", async () => {
    await bulkAddRepos([
      {
        provider: "gitlab",
        host: "gitlab.example.com",
        repo_path: "group/subgroup/project",
      },
    ]);

    const request = vi.mocked(fetch).mock.calls[0]?.[0];
    expect(request).toBeInstanceOf(Request);
    await expect((request as Request).clone().json()).resolves.toEqual({
      repos: [
        {
          provider: "gitlab",
          host: "gitlab.example.com",
          repo_path: "group/subgroup/project",
        },
      ],
    });
  });

  it("posts agent settings updates", async () => {
    await updateSettings({
      agents: [
        {
          key: "codex",
          label: "Codex",
          command: ["codex", "--full-auto"],
          enabled: true,
        },
      ],
    });

    const request = vi.mocked(fetch).mock.calls[0]?.[0];
    expect(request).toBeInstanceOf(Request);
    expect(new URL((request as Request).url).pathname).toBe(
      "/api/v1/settings",
    );
    expect((request as Request).method).toBe("PUT");
    expect((request as Request).headers.get("Content-Type")).toBe(
      "application/json",
    );
    await expect((request as Request).clone().json()).resolves.toEqual({
      agents: [
        {
          key: "codex",
          label: "Codex",
          command: ["codex", "--full-auto"],
          enabled: true,
        },
      ],
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

    await expect(previewRepos("acme", "[")).rejects.toThrow("invalid glob pattern");
  });
});
