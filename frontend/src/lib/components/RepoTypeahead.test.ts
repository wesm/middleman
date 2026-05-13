import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from "vitest";

import type { Repo } from "@middleman/ui/api/types";
import { createSettingsStore } from "../../../../packages/ui/src/stores/settings.svelte.ts";
import { client } from "../api/runtime.js";
import RepoTypeahead from "./RepoTypeahead.svelte";

const settingsStore = createSettingsStore();

vi.mock("@middleman/ui", () => ({
  getStores: () => ({
    settings: settingsStore,
  }),
}));

vi.mock("../api/runtime.js", () => ({
  client: {
    GET: vi.fn(() => Promise.resolve({ data: [], error: undefined })),
  },
}));

const getRepos = client.GET as unknown as Mock<() => Promise<{ data: Repo[]; error: undefined }>>;

describe("RepoTypeahead", () => {
  beforeEach(() => {
    settingsStore.setConfiguredRepos([]);
    getRepos.mockResolvedValue({ data: [], error: undefined });
  });

  afterEach(() => {
    cleanup();
    settingsStore.setConfiguredRepos([]);
  });

  it("updates dropdown options when configured repos change", async () => {
    render(RepoTypeahead, {
      props: {
        selected: undefined,
        onchange: vi.fn(),
      },
    });

    await fireEvent.click(screen.getByRole("button", { name: /all repos/i }));
    expect(screen.queryByRole("option", { name: /import-lab\/api/i })).toBeNull();

    settingsStore.setConfiguredRepos([
      {
        provider: "github",
        platform_host: "github.com",
        owner: "import-lab",
        name: "api",
        repo_path: "import-lab/api",
        is_glob: false,
        matched_repo_count: 1,
      },
    ]);

    await waitFor(() => {
      expect(screen.getByRole("option", { name: /import-lab\/api/i })).toBeTruthy();
    });
  });

  it("keeps fetched repos for glob-backed settings entries", async () => {
    const fetchedRepos = [
      {
        Platform: "github",
        PlatformHost: "github.com",
        Owner: "roborev-dev",
        Name: "middleman",
      },
      {
        Platform: "github",
        PlatformHost: "github.com",
        Owner: "roborev-dev",
        Name: "worker",
      },
    ] as unknown as Repo[];

    getRepos.mockResolvedValue({
      data: fetchedRepos,
      error: undefined,
    });

    settingsStore.setConfiguredRepos([
      {
        provider: "github",
        platform_host: "github.com",
        owner: "roborev-dev",
        name: "*",
        repo_path: "roborev-dev/*",
        is_glob: true,
        matched_repo_count: 2,
      },
    ]);

    render(RepoTypeahead, {
      props: {
        selected: undefined,
        onchange: vi.fn(),
      },
    });

    await fireEvent.click(screen.getByRole("button", { name: /all repos/i }));

    await waitFor(() => {
      expect(screen.getByRole("option", { name: /roborev-dev\/middleman/i })).toBeTruthy();
      expect(screen.getByRole("option", { name: /roborev-dev\/worker/i })).toBeTruthy();
    });
  });

  it("drops removed repos after settings remove matching entries", async () => {
    const fetchedRepos = [
      {
        Platform: "github",
        PlatformHost: "github.com",
        Owner: "roborev-dev",
        Name: "middleman",
      },
    ] as unknown as Repo[];

    getRepos
      .mockResolvedValueOnce({
        data: fetchedRepos,
        error: undefined,
      })
      .mockResolvedValueOnce({
        data: [],
        error: undefined,
      });

    settingsStore.setConfiguredRepos([
      {
        provider: "github",
        platform_host: "github.com",
        owner: "roborev-dev",
        name: "*",
        repo_path: "roborev-dev/*",
        is_glob: true,
        matched_repo_count: 1,
      },
    ]);

    render(RepoTypeahead, {
      props: {
        selected: undefined,
        onchange: vi.fn(),
      },
    });

    await fireEvent.click(screen.getByRole("button", { name: /all repos/i }));

    await waitFor(() => {
      expect(screen.getByRole("option", { name: /roborev-dev\/middleman/i })).toBeTruthy();
    });

    settingsStore.setConfiguredRepos([]);

    await waitFor(() => {
      expect(screen.queryByRole("option", { name: /roborev-dev\/middleman/i })).toBeNull();
    });
  });
});
