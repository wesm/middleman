import { existsSync, readFileSync } from "fs";
import { dirname, resolve } from "path";
import { fileURLToPath } from "url";
import { describe, expect, it } from "vitest";

const testDir = dirname(fileURLToPath(import.meta.url));

describe("favicon integration", () => {
  it("provides the SVG source and PNG fallback", () => {
    const svgPath = resolve(testDir, "../../public/favicon.svg");
    const pngPath = resolve(testDir, "../../public/favicon-32.png");
    const stackedPath = resolve(testDir, "../../public/favicon-mm-stacked.svg");
    const mirroredPath = resolve(testDir, "../../public/favicon-mm-mirrored.svg");
    const flowPath = resolve(testDir, "../../public/favicon-mm-flow.svg");

    expect(existsSync(svgPath)).toBe(true);
    expect(existsSync(pngPath)).toBe(true);
    expect(existsSync(stackedPath)).toBe(true);
    expect(existsSync(mirroredPath)).toBe(true);
    expect(existsSync(flowPath)).toBe(true);
    expect(readFileSync(svgPath, "utf8")).toContain("<svg");
    expect(readFileSync(stackedPath, "utf8")).toContain("<svg");
    expect(readFileSync(mirroredPath, "utf8")).toContain("<svg");
    expect(readFileSync(flowPath, "utf8")).toContain("<svg");
  });

  it("uses a single solid j-shaped arrow with a chevron head in the SVG favicon", () => {
    const svgPath = resolve(testDir, "../../public/favicon.svg");
    const svg = readFileSync(svgPath, "utf8");
    const restoredArrowPath =
      "M28.4 26V33C28.4 36.4 26.8 39 23.7 40.8L20 42.9H17.2L15.4 44.8C15.1 45.1 14.7 45.1 14.4 44.9L10.8 42.1C10.4 41.8 10.4 41.2 10.8 40.9L14.4 38.1C14.7 37.9 15.1 37.9 15.4 38.2L17.2 40.1H19.2C22.2 40.1 24.4 38.1 24.4 35V26Z";

    expect(svg).toContain(restoredArrowPath);
    expect(svg).not.toContain("M26 26v10l-6 6h12l-6-6z");
    expect(svg).not.toContain(
      "M28.4 26V32.8C28.4 36.2 26.9 39 23.8 40.8L19.8 43.2H15.7L12 41L15.7 38.2H19.5C22.5 38.2 24.4 36.1 24.4 33V26Z",
    );
  });

  it("declares the favicon assets in the app shell", () => {
    const htmlPath = resolve(testDir, "../../index.html");
    const html = readFileSync(htmlPath, "utf8");

    expect(html).toContain('rel="icon"');
    expect(html).toContain('href="/favicon.svg"');
    expect(html).toContain('href="/favicon-32.png"');
  });

  it("packs the mirrored MM variant tightly for favicon legibility", () => {
    const svgPath = resolve(testDir, "../../public/favicon-mm-mirrored.svg");
    const svg = readFileSync(svgPath, "utf8");

    expect(svg).toContain("M6.8 40V11.6H12.4");
    expect(svg).toContain("M45.2 40V11.6H39.6");
    expect(svg).not.toContain("M10.5 37V15H14.7");
  });
});
