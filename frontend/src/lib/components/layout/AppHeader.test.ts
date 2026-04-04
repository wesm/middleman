import { cleanup, fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Prevent RepoTypeahead from making real API calls in the test environment.
vi.mock("../../api/runtime.js", () => ({
  client: {
    GET: () => Promise.resolve({ data: [], error: undefined }),
  },
  apiErrorMessage: () => "",
}));

import AppHeader from "./AppHeader.svelte";
import { initTheme, cleanupTheme } from "../../stores/theme.svelte.js";

type MediaChangeCallback = (event: MediaQueryListEvent) => void;

function mockMatchMedia(matches: boolean, listeners?: MediaChangeCallback[]): void {
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn().mockImplementation((_event: string, cb: MediaChangeCallback) => {
        listeners?.push(cb);
      }),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  });
}

describe("AppHeader", () => {
  beforeEach(() => {
    document.documentElement.classList.remove("dark");
    localStorage.clear();
    mockMatchMedia(false);
  });

  afterEach(() => {
    cleanupTheme();
    cleanup();
    document.documentElement.classList.remove("dark");
    localStorage.clear();
  });

  it("toggles the root dark class when the theme button is clicked", async () => {
    initTheme();
    render(AppHeader);

    const button = screen.getByTitle("Toggle theme");

    expect(document.documentElement.classList.contains("dark")).toBe(false);

    await fireEvent.click(button);
    expect(document.documentElement.classList.contains("dark")).toBe(true);

    await fireEvent.click(button);
    expect(document.documentElement.classList.contains("dark")).toBe(false);
  });

  it("applies the system dark preference on mount", () => {
    cleanup();
    document.documentElement.classList.remove("dark");
    mockMatchMedia(true);

    initTheme();
    render(AppHeader);

    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it("persists theme choice to localStorage on toggle", async () => {
    initTheme();
    render(AppHeader);

    const button = screen.getByTitle("Toggle theme");

    await fireEvent.click(button);
    expect(localStorage.getItem("middleman-theme")).toBe("dark");

    await fireEvent.click(button);
    expect(localStorage.getItem("middleman-theme")).toBe("light");
  });

  it("restores theme from localStorage over system preference", () => {
    localStorage.setItem("middleman-theme", "dark");
    mockMatchMedia(false);

    initTheme();
    render(AppHeader);

    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it("falls back to system preference when no stored theme", () => {
    cleanup();
    document.documentElement.classList.remove("dark");
    mockMatchMedia(true);

    initTheme();
    render(AppHeader);

    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it("ignores invalid localStorage value and falls back to system preference", () => {
    cleanup();
    document.documentElement.classList.remove("dark");
    localStorage.setItem("middleman-theme", "garbage");
    mockMatchMedia(true);

    initTheme();
    render(AppHeader);

    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(localStorage.getItem("middleman-theme")).toBeNull();
  });

  it("falls back to system preference when localStorage throws", () => {
    cleanup();
    document.documentElement.classList.remove("dark");
    mockMatchMedia(true);

    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("blocked");
    });

    initTheme();
    render(AppHeader);

    expect(document.documentElement.classList.contains("dark")).toBe(true);

    vi.restoreAllMocks();
  });

  it("toggle still works when localStorage.setItem throws", async () => {
    initTheme();

    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("blocked");
    });

    render(AppHeader);

    const button = screen.getByTitle("Toggle theme");

    await fireEvent.click(button);
    expect(document.documentElement.classList.contains("dark")).toBe(true);

    vi.restoreAllMocks();
  });
});
