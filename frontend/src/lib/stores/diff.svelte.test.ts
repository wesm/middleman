import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

describe("diff store localStorage guards", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("initializes tab width to default when localStorage throws", async () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("blocked");
    });

    // Re-import the module to trigger initialization with throwing localStorage.
    // vitest caches modules, so we need to reset first.
    vi.resetModules();
    const { getTabWidth } = await import("./diff.svelte.js");

    expect(getTabWidth()).toBe(4);
  });

  it("initializes hideWhitespace to false when localStorage throws", async () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("blocked");
    });

    vi.resetModules();
    const { getHideWhitespace } = await import("./diff.svelte.js");

    expect(getHideWhitespace()).toBe(false);
  });

  it("setTabWidth does not throw when localStorage.setItem throws", async () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("blocked");
    });

    vi.resetModules();
    const { setTabWidth, getTabWidth } = await import("./diff.svelte.js");

    expect(() => setTabWidth(2)).not.toThrow();
    expect(getTabWidth()).toBe(2);
  });

  it("setHideWhitespace does not throw when localStorage.setItem throws", async () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("blocked");
    });

    vi.resetModules();
    const { setHideWhitespace, getHideWhitespace } = await import("./diff.svelte.js");

    expect(() => setHideWhitespace(true)).not.toThrow();
    expect(getHideWhitespace()).toBe(true);
  });

  it("collapsed files load as empty when localStorage throws", async () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("blocked");
    });

    vi.resetModules();
    const { isFileCollapsed } = await import("./diff.svelte.js");

    expect(isFileCollapsed("owner", "repo", 1, "file.ts")).toBe(false);
  });

  it("toggleFileCollapsed does not throw when localStorage.setItem throws", async () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("blocked");
    });

    vi.resetModules();
    const { toggleFileCollapsed, isFileCollapsed } = await import("./diff.svelte.js");

    expect(() => toggleFileCollapsed("owner", "repo", 1, "file.ts")).not.toThrow();
    expect(isFileCollapsed("owner", "repo", 1, "file.ts")).toBe(true);
  });
});
