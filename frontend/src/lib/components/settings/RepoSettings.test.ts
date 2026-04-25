import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";

const mockRefreshSyncStatus = vi.fn();
let mockEmbedded = false;

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
  isEmbedded: () => mockEmbedded,
}));

import { bulkAddRepos, previewRepos } from "../../api/settings.js";
import RepoSettings from "./RepoSettings.svelte";

const mockPreviewRepos = vi.mocked(previewRepos);
const mockBulkAddRepos = vi.mocked(bulkAddRepos);

describe("RepoSettings", () => {
  afterEach(() => {
    cleanup();
    mockRefreshSyncStatus.mockReset();
    mockPreviewRepos.mockReset();
    mockBulkAddRepos.mockReset();
    mockEmbedded = false;
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

  it("opens the repository import modal and restores focus on close", async () => {
    render(RepoSettings, {
      props: {
        repos: [],
        onUpdate: vi.fn(),
      },
    });

    const trigger = screen.getByRole("button", { name: "Add repositories…" });
    await fireEvent.click(trigger);

    expect(screen.getByRole("dialog", { name: "Add repositories" })).toBeTruthy();
    expect(screen.getByLabelText("Repository pattern")).toBeTruthy();

    await fireEvent.click(screen.getByRole("button", { name: "Close" }));
    await waitFor(() => expect(document.activeElement).toBe(trigger));
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

  it("hides import and direct add controls in embedded mode", () => {
    mockEmbedded = true;
    render(RepoSettings, {
      props: {
        repos: [],
        onUpdate: vi.fn(),
      },
    });

    expect(screen.queryByRole("button", { name: "Add repositories…" })).toBeNull();
    expect(screen.queryByText("Advanced: add exact repo or tracking glob directly")).toBeNull();
  });

  it("updates repos and refreshes sync status after import", async () => {
    const importedRepos = [{
      owner: "acme",
      name: "api",
      is_glob: false,
      matched_repo_count: 1,
    }];
    const onUpdate = vi.fn();
    mockPreviewRepos.mockResolvedValue({
      owner: "acme",
      pattern: "*",
      repos: [{
        owner: "acme",
        name: "api",
        description: "HTTP API",
        private: false,
        pushed_at: null,
        already_configured: false,
      }],
    });
    mockBulkAddRepos.mockResolvedValue({
      repos: importedRepos,
      activity: {
        view_mode: "threaded",
        time_range: "7d",
        hide_closed: false,
        hide_bots: false,
      },
      terminal: { font_family: "" },
    });
    render(RepoSettings, {
      props: {
        repos: [],
        onUpdate,
      },
    });

    await fireEvent.click(screen.getByRole("button", { name: "Add repositories…" }));
    await fireEvent.input(screen.getByLabelText("Repository pattern"), { target: { value: "acme/*" } });
    await fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    await screen.findByText("acme/api");
    await fireEvent.click(screen.getByRole("button", { name: "Add selected repositories" }));

    await waitFor(() => expect(onUpdate).toHaveBeenCalledWith(importedRepos));
    expect(mockRefreshSyncStatus).toHaveBeenCalled();
  });
});
