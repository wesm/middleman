import { cleanup, fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import AppHeader from "./AppHeader.svelte";

function mockMatchMedia(matches: boolean): void {
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  });
}

describe("AppHeader", () => {
  beforeEach(() => {
    document.documentElement.classList.remove("dark");
    mockMatchMedia(false);
  });

  afterEach(() => {
    cleanup();
    document.documentElement.classList.remove("dark");
  });

  it("toggles the root dark class when the theme button is clicked", async () => {
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

    render(AppHeader);

    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });
});
