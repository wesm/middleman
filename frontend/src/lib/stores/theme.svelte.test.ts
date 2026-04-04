import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  initTheme,
  isDark,
  toggleTheme,
  isThemeToggleVisible,
  applyThemeOverrides,
  cleanupTheme,
} from "./theme.svelte.js";

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

beforeEach(() => {
  mockMatchMedia(false);
});

afterEach(() => {
  delete window.__middleman_config;
  document.documentElement.classList.remove("dark");
  document.documentElement.style.cssText = "";
  try { localStorage.removeItem("middleman-theme"); } catch { /* storage blocked */ }
  cleanupTheme();
});

describe("standalone mode (no config)", () => {
  it("toggle is visible when no theme.mode set", () => {
    initTheme();
    expect(isThemeToggleVisible()).toBe(true);
  });

  it("toggleTheme flips dark state", () => {
    initTheme();
    const initial = isDark();
    toggleTheme();
    expect(isDark()).toBe(!initial);
  });

  it("persists theme to localStorage", () => {
    initTheme();
    toggleTheme();
    const stored = localStorage.getItem("middleman-theme");
    expect(stored).toBeTruthy();
  });
});

describe("embedded mode with theme.mode", () => {
  it("hides toggle when mode is set", () => {
    window.__middleman_config = { theme: { mode: "dark" } };
    window.__middleman_notify_config_changed?.();
    initTheme();
    expect(isThemeToggleVisible()).toBe(false);
  });

  it("applies dark class when mode is dark", () => {
    window.__middleman_config = { theme: { mode: "dark" } };
    window.__middleman_notify_config_changed?.();
    initTheme();
    expect(isDark()).toBe(true);
    expect(
      document.documentElement.classList.contains("dark"),
    ).toBe(true);
  });

  it("applies light class when mode is light", () => {
    window.__middleman_config = { theme: { mode: "light" } };
    window.__middleman_notify_config_changed?.();
    initTheme();
    expect(isDark()).toBe(false);
  });
});

describe("applyThemeOverrides", () => {
  it("sets CSS variables from color config", () => {
    applyThemeOverrides(
      { bgPrimary: "#111", accentBlue: "#00f" },
      undefined,
      undefined,
    );
    const style = document.documentElement.style;
    expect(style.getPropertyValue("--bg-primary")).toBe("#111");
    expect(style.getPropertyValue("--accent-blue")).toBe("#00f");
  });

  it("sets font CSS variables", () => {
    applyThemeOverrides(undefined, { sans: "SF Pro" }, undefined);
    expect(
      document.documentElement.style.getPropertyValue("--font-sans"),
    ).toBe("SF Pro");
  });

  it("sets radius CSS variables", () => {
    applyThemeOverrides(undefined, undefined, { sm: "2px" });
    expect(
      document.documentElement.style.getPropertyValue("--radius-sm"),
    ).toBe("2px");
  });
});
