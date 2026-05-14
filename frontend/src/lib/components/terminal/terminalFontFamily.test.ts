import { describe, expect, it } from "vitest";

import { buildTerminalFontFamily } from "./terminalFontFamily.js";

describe("buildTerminalFontFamily", () => {
  const defaultStack = '"JetBrains Mono", "SF Mono", Menlo, Consolas, monospace';

  it("uses the default fallback stack when no font is configured", () => {
    expect(buildTerminalFontFamily("", defaultStack)).toBe(defaultStack);
    expect(buildTerminalFontFamily("   ", defaultStack)).toBe(defaultStack);
  });

  it("prepends a configured font to the default fallback stack", () => {
    expect(buildTerminalFontFamily('"MesloLGS NF"', defaultStack)).toBe([
      '"MesloLGS NF"',
      '"JetBrains Mono"',
      '"SF Mono"',
      "Menlo",
      "Consolas",
      "monospace",
    ].join(", "));
  });

  it("keeps configured font candidates ahead of the default stack and leaves generic fallbacks last", () => {
    expect(buildTerminalFontFamily('"Fira Code", monospace', defaultStack)).toBe([
      '"Fira Code"',
      '"JetBrains Mono"',
      '"SF Mono"',
      "Menlo",
      "Consolas",
      "monospace",
    ].join(", "));
  });

  it("preserves multiple configured concrete fonts before default mobile-friendly fallbacks", () => {
    expect(
      buildTerminalFontFamily(
        '"MesloLGS NF", "Symbols Nerd Font Mono", monospace',
        defaultStack,
      ),
    ).toBe([
      '"MesloLGS NF"',
      '"Symbols Nerd Font Mono"',
      '"JetBrains Mono"',
      '"SF Mono"',
      "Menlo",
      "Consolas",
      "monospace",
    ].join(", "));
  });
});
