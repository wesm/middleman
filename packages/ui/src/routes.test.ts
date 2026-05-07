import { describe, expect, it } from "vitest";
import {
  buildFocusIssueRoute,
  buildFocusListRoute,
  buildFocusPullRequestRoute,
  buildIssueRoute,
  buildProviderIssueRoute,
  buildProviderPullRequestFilesRoute,
  buildProviderPullRequestRoute,
  buildPullRequestFilesRoute,
  buildPullRequestRoute,
  buildRoutedItemRoute,
} from "./routes.js";

const githubWidgets = {
  provider: "github",
  platformHost: "github.com",
  owner: "acme",
  name: "widgets",
  repoPath: "acme/widgets",
} as const;

describe("route item builders", () => {
  it("builds pull request conversation and files routes from provider repo identity", () => {
    const ref = { ...githubWidgets, number: 42 };

    expect(buildPullRequestRoute(ref)).toBe(
      "/pulls/detail?provider=github&platform_host=github.com&repo_path=acme%2Fwidgets&number=42",
    );
    expect(buildPullRequestFilesRoute(ref)).toBe(
      "/pulls/detail/files?provider=github&platform_host=github.com&repo_path=acme%2Fwidgets&number=42",
    );
  });

  it("builds issue routes with encoded platform hosts", () => {
    expect(
      buildIssueRoute({
        ...githubWidgets,
        platformHost: "ghe.example.com/team one",
        number: 7,
      }),
    ).toBe(
      "/issues/detail?provider=github&platform_host=ghe.example.com%2Fteam%20one&repo_path=acme%2Fwidgets&number=7",
    );
  });

  it("omits absent issue platform host query strings", () => {
    expect(
      buildIssueRoute({
        provider: "github",
        owner: "acme",
        name: "widgets",
        repoPath: "acme/widgets",
        number: 7,
      }),
    ).toBe(
      "/issues/detail?provider=github&repo_path=acme%2Fwidgets&number=7",
    );
  });

  it("builds provider repo-path routes with escaped refs", () => {
    const deep = {
      provider: "gitlab",
      platformHost: "gitlab.example.com:8443",
      repoPath: "Group/SubGroup/SubGroup 2/My_Project.v2",
      number: 12,
    };

    expect(buildProviderPullRequestRoute(deep)).toBe(
      "/pulls/detail?provider=gitlab&platform_host=gitlab.example.com%3A8443&repo_path=Group%2FSubGroup%2FSubGroup%202%2FMy_Project.v2&number=12",
    );
    expect(buildProviderPullRequestFilesRoute(deep)).toBe(
      "/pulls/detail/files?provider=gitlab&platform_host=gitlab.example.com%3A8443&repo_path=Group%2FSubGroup%2FSubGroup%202%2FMy_Project.v2&number=12",
    );
    expect(buildProviderIssueRoute(deep)).toBe(
      "/issues/detail?provider=gitlab&platform_host=gitlab.example.com%3A8443&repo_path=Group%2FSubGroup%2FSubGroup%202%2FMy_Project.v2&number=12",
    );
  });

  it("builds focus item and list routes", () => {
    expect(
      buildFocusPullRequestRoute({
        ...githubWidgets,
        number: 42,
      }),
    ).toBe(
      "/focus/pr?provider=github&platform_host=github.com&repo_path=acme%2Fwidgets&number=42",
    );
    expect(
      buildFocusIssueRoute({
        ...githubWidgets,
        platformHost: "ghe.example.com",
        number: 7,
      }),
    ).toBe(
      "/focus/issue?provider=github&platform_host=ghe.example.com&repo_path=acme%2Fwidgets&number=7",
    );
    expect(buildFocusListRoute({ itemType: "mrs" })).toBe("/focus/mrs");
    expect(buildFocusListRoute({ itemType: "issues" })).toBe("/focus/issues");
    expect(buildFocusListRoute({ itemType: "mrs", repo: "acme/widgets" })).toBe(
      "/focus/mrs?repo=acme%2Fwidgets",
    );
  });

  it("builds routed item routes for normal and focus surfaces", () => {
    const pr = { itemType: "pr", ...githubWidgets, number: 42 } as const;
    const issue = {
      itemType: "issue",
      ...githubWidgets,
      platformHost: "ghe.example.com",
      number: 7,
    } as const;

    expect(buildRoutedItemRoute(pr)).toBe(
      "/pulls/detail?provider=github&platform_host=github.com&repo_path=acme%2Fwidgets&number=42",
    );
    expect(buildRoutedItemRoute(issue)).toBe(
      "/issues/detail?provider=github&platform_host=ghe.example.com&repo_path=acme%2Fwidgets&number=7",
    );
    expect(buildRoutedItemRoute(pr, { focus: true })).toBe(
      "/focus/pr?provider=github&platform_host=github.com&repo_path=acme%2Fwidgets&number=42",
    );
    expect(buildRoutedItemRoute(issue, { focus: true })).toBe(
      "/focus/issue?provider=github&platform_host=ghe.example.com&repo_path=acme%2Fwidgets&number=7",
    );
  });
});
