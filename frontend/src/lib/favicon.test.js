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

  it("declares the favicon assets in the app shell", () => {
    const htmlPath = resolve(testDir, "../../index.html");
    const html = readFileSync(htmlPath, "utf8");

    expect(html).toContain('rel="icon"');
    expect(html).toContain('href="/favicon.svg"');
    expect(html).toContain('href="/favicon-32.png"');
  });
});
