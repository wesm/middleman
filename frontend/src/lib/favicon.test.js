import { existsSync, readFileSync } from "fs";
import { dirname, resolve } from "path";
import { fileURLToPath } from "url";
import { describe, expect, it } from "vitest";

const testDir = dirname(fileURLToPath(import.meta.url));

describe("favicon integration", () => {
  it("provides the SVG source and PNG fallback", () => {
    const svgPath = resolve(testDir, "../../public/favicon.svg");
    const pngPath = resolve(testDir, "../../public/favicon-32.png");

    expect(existsSync(svgPath)).toBe(true);
    expect(existsSync(pngPath)).toBe(true);
    expect(readFileSync(svgPath, "utf8")).toContain("<svg");
  });

  it("uses a single solid j-shaped arrow outline in the SVG favicon", () => {
    const svgPath = resolve(testDir, "../../public/favicon.svg");
    const svg = readFileSync(svgPath, "utf8");
    const restoredArrowPath =
      "M28.4 26V32.8C28.4 36.2 26.9 39 23.8 40.8L19.8 43.2H15.7L12 41L15.7 38.2H19.5C22.5 38.2 24.4 36.1 24.4 33V26Z";

    expect(svg).toContain(restoredArrowPath);
    expect(svg).not.toContain("M26 26v10l-6 6h12l-6-6z");
    expect(svg).not.toContain("L17.5 41.1L15.5 43.8");
  });

  it("declares the favicon assets in the app shell", () => {
    const htmlPath = resolve(testDir, "../../index.html");
    const html = readFileSync(htmlPath, "utf8");

    expect(html).toContain('rel="icon"');
    expect(html).toContain('href="/favicon.svg"');
    expect(html).toContain('href="/favicon-32.png"');
  });
});
