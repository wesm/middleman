import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { createSettingsStore } from "../../../../packages/ui/src/stores/settings.svelte.ts";

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

import RepoTypeahead from "./RepoTypeahead.svelte";

describe("RepoTypeahead", () => {
  beforeEach(() => {
    settingsStore.setConfiguredRepos([]);
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
});
