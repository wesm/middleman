import { cleanup, fireEvent, render, screen } from "@testing-library/svelte";
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
  previewRepos: vi.fn(),
  bulkAddRepos: vi.fn(),
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

  it("opens the repository import modal", async () => {
    render(RepoSettings, {
      props: {
        repos: [],
        onUpdate: vi.fn(),
      },
    });

    await fireEvent.click(screen.getByRole("button", { name: "Add repositories…" }));

    expect(screen.getByRole("dialog", { name: "Add repositories" })).toBeTruthy();
    expect(screen.getByLabelText("Repository pattern")).toBeTruthy();
  });

  it("keeps direct glob add in an advanced section", () => {
    render(RepoSettings, {
      props: {
        repos: [],
        onUpdate: vi.fn(),
      },
    });

    const summary = screen.getByText("Advanced: add exact repo or tracking glob directly");
    expect(summary).toBeTruthy();
    expect(summary.closest("details")?.hasAttribute("open")).toBe(false);
  });
});
