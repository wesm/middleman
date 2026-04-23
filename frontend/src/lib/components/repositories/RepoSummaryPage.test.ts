import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/svelte";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";

const mockGet = vi.fn();
const mockPost = vi.fn();
const mockNavigate = vi.fn();
const mockSetGlobalRepo = vi.fn();

vi.mock("../../api/runtime.js", () => ({
  client: {
    GET: (...args: unknown[]) => mockGet(...args),
    POST: (...args: unknown[]) => mockPost(...args),
  },
  apiErrorMessage: (
    error: { detail?: string; title?: string } | undefined,
    fallback: string,
  ) => error?.detail ?? error?.title ?? fallback,
}));

vi.mock("../../stores/router.svelte.js", () => ({
  navigate: (path: string) => mockNavigate(path),
}));

vi.mock("../../stores/filter.svelte.js", () => ({
  setGlobalRepo: (repo: string | undefined) =>
    mockSetGlobalRepo(repo),
}));

vi.mock("@middleman/ui", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@middleman/ui")>();
  const { default: MockCommentEditor } = await import(
    "../../../test/MockCommentEditor.svelte"
  );
  return {
    ...actual,
    CommentEditor: MockCommentEditor,
    getStores: () => ({
      sync: {
        subscribeSyncComplete: () => () => {},
      },
      settings: {
        isSettingsLoaded: () => true,
        hasConfiguredRepos: () => true,
      },
    }),
  };
});

import RepoSummaryPage from "./RepoSummaryPage.svelte";

describe("RepoSummaryPage", () => {
  beforeEach(() => {
    mockGet.mockReset();
    mockPost.mockReset();
    mockNavigate.mockReset();
    mockSetGlobalRepo.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("renders repository summaries from the API", async () => {
    mockGet.mockResolvedValue({
      data: [{
        owner: "acme",
        name: "widgets",
        platform_host: "github.com",
        cached_pr_count: 6,
        open_pr_count: 3,
        draft_pr_count: 1,
        cached_issue_count: 4,
        open_issue_count: 2,
        most_recent_activity_at: "2026-04-17T15:04:05Z",
        last_sync_completed_at: "2026-04-17T15:00:00Z",
        last_sync_started_at: "2026-04-17T14:59:00Z",
        last_sync_error: "",
        active_authors: [
          { login: "alice", item_count: 3 },
          { login: "bob", item_count: 2 },
        ],
        recent_issues: [{
          number: 12,
          title: "Investigate repo summary card",
          author: "alice",
          state: "open",
          url: "https://github.com/acme/widgets/issues/12",
          last_activity_at: "2026-04-17T14:55:00Z",
        }],
      }],
      error: undefined,
    });

    render(RepoSummaryPage);

    expect(await screen.findByText("acme/widgets")).toBeTruthy();
    expect(screen.getAllByText("Open PRs")).toHaveLength(2);
    expect(
      screen.getByText("Investigate repo summary card"),
    ).toBeTruthy();
    expect(screen.getByText("alice")).toBeTruthy();
  });

  it("shows cached output and sync errors clearly", async () => {
    mockGet.mockResolvedValue({
      data: [{
        owner: "acme",
        name: "widgets",
        platform_host: "github.com",
        cached_pr_count: 6,
        open_pr_count: 3,
        draft_pr_count: 1,
        cached_issue_count: 4,
        open_issue_count: 2,
        most_recent_activity_at: "2026-04-17T15:04:05Z",
        last_sync_completed_at: "2026-04-17T15:00:00Z",
        last_sync_started_at: "2026-04-17T14:59:00Z",
        last_sync_error: "rate limit exceeded",
        active_authors: [],
        recent_issues: [],
      }],
      error: undefined,
    });

    render(RepoSummaryPage);

    await screen.findByText("acme/widgets");

    expect(screen.getByText("Cached PRs")).toBeTruthy();
    expect(screen.getByText("Cached issues")).toBeTruthy();
    expect(screen.getByText("rate limit exceeded")).toBeTruthy();
  });

  it("navigates from repo metric cells instead of separate view buttons", async () => {
    mockGet.mockResolvedValue({
      data: [{
        owner: "acme",
        name: "widgets",
        platform_host: "github.com",
        cached_pr_count: 6,
        open_pr_count: 3,
        draft_pr_count: 1,
        cached_issue_count: 4,
        open_issue_count: 2,
        most_recent_activity_at: "2026-04-17T15:04:05Z",
        last_sync_completed_at: "2026-04-17T15:00:00Z",
        last_sync_started_at: "2026-04-17T14:59:00Z",
        last_sync_error: "",
        active_authors: [],
        recent_issues: [],
      }],
      error: undefined,
    });

    render(RepoSummaryPage);

    await screen.findByText("acme/widgets");

    expect(
      screen.queryByRole("button", { name: "View PRs" }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "View issues" }),
    ).toBeNull();

    await fireEvent.click(
      screen.getByRole("button", { name: /3\s+Open PRs/ }),
    );
    expect(mockSetGlobalRepo).toHaveBeenCalledWith(
      "acme/widgets",
    );
    expect(mockNavigate).toHaveBeenCalledWith("/pulls");

    await fireEvent.click(
      screen.getByRole("button", { name: /2\s+Open issues/ }),
    );
    expect(mockSetGlobalRepo).toHaveBeenCalledWith(
      "acme/widgets",
    );
    expect(mockNavigate).toHaveBeenCalledWith("/issues");
  });

  it("creates an issue from a repo card and navigates to it", async () => {
    mockGet.mockResolvedValue({
      data: [{
        owner: "acme",
        name: "widgets",
        platform_host: "github.com",
        cached_pr_count: 2,
        open_pr_count: 1,
        draft_pr_count: 0,
        cached_issue_count: 1,
        open_issue_count: 1,
        most_recent_activity_at: "2026-04-17T15:04:05Z",
        last_sync_completed_at: "2026-04-17T15:00:00Z",
        last_sync_started_at: "2026-04-17T14:59:00Z",
        last_sync_error: "",
        active_authors: [],
        recent_issues: [],
      }],
      error: undefined,
    });
    mockPost.mockResolvedValue({
      data: {
        Number: 27,
        repo_owner: "acme",
        repo_name: "widgets",
        detail_loaded: false,
      },
      error: undefined,
    });

    render(RepoSummaryPage);

    await screen.findByText("acme/widgets");
    await fireEvent.click(
      screen.getByRole("button", { name: "New issue" }),
    );

    expect(
      screen.getByRole("dialog", {
        name: "New issue in acme/widgets",
      }),
    ).toBeTruthy();
    const bodyEditor = screen.getByTestId("mock-comment-editor");
    expect(bodyEditor.getAttribute("data-owner")).toBe("acme");
    expect(bodyEditor.getAttribute("data-name")).toBe("widgets");

    await fireEvent.input(
      screen.getByPlaceholderText("Issue title"),
      {
        target: { value: "Ship repo summaries" },
      },
    );
    await fireEvent.input(
      screen.getByRole("textbox", {
        name: "Describe the problem, context, or follow-up work",
      }),
      {
        target: { value: "Need a compact repo dashboard." },
      },
    );
    await fireEvent.submit(
      screen.getByRole("button", { name: "Create issue" }),
    );

    await waitFor(() => {
      expect(mockPost).toHaveBeenCalledWith(
        "/repos/{owner}/{name}/issues",
        expect.objectContaining({
          params: {
            path: { owner: "acme", name: "widgets" },
          },
          body: {
            title: "Ship repo summaries",
            body: "Need a compact repo dashboard.",
          },
        }),
      );
      expect(mockSetGlobalRepo).toHaveBeenCalledWith(
        "acme/widgets",
      );
      expect(mockNavigate).toHaveBeenCalledWith(
        "/issues/acme/widgets/27",
      );
    });
  });

  it("retains modal issue drafts when dismissed", async () => {
    mockGet.mockResolvedValue({
      data: [{
        owner: "acme",
        name: "widgets",
        platform_host: "github.com",
        cached_pr_count: 2,
        open_pr_count: 1,
        draft_pr_count: 0,
        cached_issue_count: 1,
        open_issue_count: 1,
        most_recent_activity_at: "2026-04-17T15:04:05Z",
        last_sync_completed_at: "2026-04-17T15:00:00Z",
        last_sync_started_at: "2026-04-17T14:59:00Z",
        last_sync_error: "",
        active_authors: [],
        recent_issues: [],
      }],
      error: undefined,
    });

    render(RepoSummaryPage);

    await screen.findByText("acme/widgets");
    await fireEvent.click(
      screen.getByRole("button", { name: "New issue" }),
    );
    await fireEvent.input(
      screen.getByPlaceholderText("Issue title"),
      { target: { value: "Draft issue title" } },
    );
    await fireEvent.input(
      screen.getByRole("textbox", {
        name: "Describe the problem, context, or follow-up work",
      }),
      {
        target: { value: "Draft issue body with @alice" },
      },
    );
    await fireEvent.click(
      screen.getByRole("button", { name: "Cancel" }),
    );

    expect(screen.queryByRole("dialog")).toBeNull();

    await fireEvent.click(
      screen.getByRole("button", { name: "New issue" }),
    );

    expect(
      (screen.getByPlaceholderText("Issue title") as HTMLInputElement)
        .value,
    ).toBe("Draft issue title");
    expect(
      (
        screen.getByRole("textbox", {
          name: "Describe the problem, context, or follow-up work",
        }) as HTMLTextAreaElement
      ).value,
    ).toBe("Draft issue body with @alice");
  });
});
