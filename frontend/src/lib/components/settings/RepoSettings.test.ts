import { cleanup, render, screen } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";

const mockRefreshSyncStatus = vi.fn();

vi.mock("@middleman/ui", () => ({
  getStores: () => ({
    sync: {
      refreshSyncStatus: mockRefreshSyncStatus,
    },
  }),
}));

vi.mock("../../api/settings.js", () => ({
  addRepo: vi.fn(),
  removeRepo: vi.fn(),
  getSettings: vi.fn(),
  refreshRepo: vi.fn(),
}));

vi.mock("../../stores/embed-config.svelte.js", () => ({
  isEmbedded: () => false,
}));

import RepoSettings from "./RepoSettings.svelte";

describe("RepoSettings", () => {
  afterEach(() => {
    cleanup();
    mockRefreshSyncStatus.mockReset();
  });

  it("renders the glob count and refresh action", () => {
    render(RepoSettings, {
      props: {
        repos: [{
          owner: "roborev-dev",
          name: "*",
          is_glob: true,
          matched_repo_count: 2,
        }],
        onUpdate: vi.fn(),
      },
    });

    expect(screen.getByText((_, element) => element?.textContent === "roborev-dev/* (2)")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Refresh" })).toBeTruthy();
  });
});
