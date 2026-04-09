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

  it("uses the restored j-shaped arrow in the SVG favicon", () => {
    const svgPath = resolve(testDir, "../../public/favicon.svg");
    const svg = readFileSync(svgPath, "utf8");
    const restoredArrowPath =
      "M28.4 26V32.8C28.4 35.8 27 38.1 24.4 39.8L22 41.3H14.9L17.3 38.3C17.8 37.8 17.3 37 16.6 37L12.2 40.4C11.5 40.9 11.5 41.9 12.2 42.4L16.6 45.8C17.3 46.3 17.8 45.5 17.3 45L14.9 42H20.7C22.3 42 23.6 41.5 24.9 40.6C27.7 38.8 29.2 35.9 29.2 32.3V26H24.4V32.7C24.4 34.5 23.8 35.9 22.4 36.9L20 38.4H15.5L17.5 41.1L15.5 43.8H20.4C22.9 43.8 24.8 43.1 26.6 41.9C29.9 39.8 31.6 36.5 31.6 32.3V26Z";

    expect(svg).toContain(restoredArrowPath);
    expect(svg).not.toContain("M26 26v10l-6 6h12l-6-6z");
  });

  it("declares the favicon assets in the app shell", () => {
    const htmlPath = resolve(testDir, "../../index.html");
    const html = readFileSync(htmlPath, "utf8");

    expect(html).toContain('rel="icon"');
    expect(html).toContain('href="/favicon.svg"');
    expect(html).toContain('href="/favicon-32.png"');
  });
});
