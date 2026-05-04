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

describe("route item builders", () => {
  it("builds pull request conversation and files routes from a named ref", () => {
    const ref = { owner: "acme", name: "widgets", number: 42 };

    expect(buildPullRequestRoute(ref)).toBe("/pulls/acme/widgets/42");
    expect(buildPullRequestFilesRoute(ref)).toBe("/pulls/acme/widgets/42/files");
  });

  it("builds issue routes with encoded platform hosts", () => {
    expect(
      buildIssueRoute({
        owner: "acme",
        name: "widgets",
        number: 7,
        platformHost: "ghe.example.com/team one",
      }),
    ).toBe("/issues/acme/widgets/7?platform_host=ghe.example.com%2Fteam%20one");
  });

  it("omits empty issue platform host query strings", () => {
    expect(
      buildIssueRoute({
        owner: "acme",
        name: "widgets",
        number: 7,
      }),
    ).toBe("/issues/acme/widgets/7");
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
        owner: "acme",
        name: "widgets",
        number: 42,
      }),
    ).toBe("/focus/pr/acme/widgets/42");
    expect(
      buildFocusIssueRoute({
        owner: "acme",
        name: "widgets",
        number: 7,
        platformHost: "ghe.example.com",
      }),
    ).toBe("/focus/issue/acme/widgets/7?platform_host=ghe.example.com");
    expect(buildFocusListRoute({ itemType: "mrs", repo: "acme/widgets" })).toBe(
      "/focus/mrs?repo=acme%2Fwidgets",
    );
    expect(buildFocusListRoute({ itemType: "issues" })).toBe("/focus/issues");
  });

  it("builds routed item routes for normal and focus surfaces", () => {
    const pr = {
      itemType: "pr",
      owner: "acme",
      name: "widgets",
      number: 42,
    } as const;
    const issue = {
      itemType: "issue",
      owner: "acme",
      name: "widgets",
      number: 7,
      platformHost: "ghe.example.com",
    } as const;

    expect(buildRoutedItemRoute(pr)).toBe("/pulls/acme/widgets/42");
    expect(buildRoutedItemRoute(pr, { focus: true })).toBe("/focus/pr/acme/widgets/42");
    expect(buildRoutedItemRoute(issue)).toBe(
      "/issues/acme/widgets/7?platform_host=ghe.example.com",
    );
    expect(buildRoutedItemRoute(issue, { focus: true })).toBe(
      "/focus/issue/acme/widgets/7?platform_host=ghe.example.com",
    );
  });
});
