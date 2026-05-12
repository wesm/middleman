import { cleanup, render, screen } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import WorkspaceListSidebar from "./WorkspaceListSidebar.svelte";

const mockGet = vi.fn();
const mockNavigate = vi.fn();

vi.mock("../../api/runtime.js", () => ({
  client: {
    GET: (...args: unknown[]) => mockGet(...args),
  },
}));

vi.mock("../../stores/router.svelte.ts", () => ({
  navigate: (path: string) => mockNavigate(path),
}));

class MockEventSource {
  addEventListener = vi.fn();
  close = vi.fn();

  constructor(readonly url: string) {}
}

interface WorkspaceFixtureOptions {
  id: string;
  provider: string;
  platformHost: string;
  owner: string;
  name: string;
  number: number;
}

function workspaceFixture({
  id,
  provider,
  platformHost,
  owner,
  name,
  number,
}: WorkspaceFixtureOptions) {
  return {
    id,
    repo: {
      provider,
      platform_host: platformHost,
      owner,
      name,
      repo_path: `${owner}/${name}`,
    },
    platform_host: platformHost,
    repo_owner: owner,
    repo_name: name,
    item_type: "pull_request",
    item_number: number,
    git_head_ref: `feature-${number}`,
    worktree_path: `/tmp/${id}`,
    tmux_session: id,
    status: "ready",
    created_at: "2026-05-12T12:00:00Z",
    mr_title: `PR ${number}`,
    mr_state: "open",
  };
}

describe("WorkspaceListSidebar", () => {
  beforeEach(() => {
    mockGet.mockReset();
    mockNavigate.mockReset();
    vi.stubGlobal("EventSource", MockEventSource);
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("shows provider icons in repo groups when multiple providers are present", async () => {
    mockGet.mockResolvedValue({
      data: {
        workspaces: [
          workspaceFixture({
            id: "ws-github",
            provider: "github",
            platformHost: "github.com",
            owner: "acme",
            name: "widgets",
            number: 42,
          }),
          workspaceFixture({
            id: "ws-gitlab",
            provider: "gitlab",
            platformHost: "gitlab.com",
            owner: "platform",
            name: "api",
            number: 7,
          }),
        ],
      },
    });

    render(WorkspaceListSidebar, { props: { selectedId: "ws-github" } });

    await screen.findByText("acme/widgets");
    expect(screen.getByRole("img", { name: "GitHub" })).toBeTruthy();
    expect(screen.getByRole("img", { name: "GitLab" })).toBeTruthy();
  });

  it("hides provider icons in repo groups when one provider is present", async () => {
    mockGet.mockResolvedValue({
      data: {
        workspaces: [
          workspaceFixture({
            id: "ws-github",
            provider: "github",
            platformHost: "github.com",
            owner: "acme",
            name: "widgets",
            number: 42,
          }),
          workspaceFixture({
            id: "ws-ghe",
            provider: "github",
            platformHost: "ghe.example.com",
            owner: "enterprise",
            name: "service",
            number: 9,
          }),
        ],
      },
    });

    render(WorkspaceListSidebar, { props: { selectedId: "ws-github" } });

    await screen.findByText("acme/widgets");
    expect(screen.queryByRole("img", { name: "GitHub" })).toBeNull();
  });
});
