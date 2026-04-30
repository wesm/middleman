import { describe, expect, it } from "vitest";

import { repoKey, shouldShowPlatformHost } from "./repoSummary.js";

describe("repo summary labels", () => {
  it("hides github.com when it is the default platform host", () => {
    const summary = {
      platform_host: "github.com",
      default_platform_host: "github.com",
      owner: "acme",
      name: "widgets",
    };

    expect(shouldShowPlatformHost(summary)).toBe(false);
    expect(repoKey(summary)).toBe("acme/widgets");
  });

  it("hides a configured non-github default platform host", () => {
    const summary = {
      platform_host: "ghe.example.com",
      default_platform_host: "ghe.example.com",
      owner: "acme",
      name: "widgets",
    };

    expect(shouldShowPlatformHost(summary)).toBe(false);
    expect(repoKey(summary)).toBe("acme/widgets");
  });

  it("includes a non-default platform host in repository labels", () => {
    const summary = {
      platform_host: "ghe.example.com",
      default_platform_host: "github.com",
      owner: "acme",
      name: "widgets",
    };

    expect(shouldShowPlatformHost(summary)).toBe(true);
    expect(repoKey(summary)).toBe("ghe.example.com/acme/widgets");
  });
});
