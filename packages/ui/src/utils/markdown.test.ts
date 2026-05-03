import { describe, expect, it } from "vitest";
import { renderMarkdown } from "./markdown.js";

describe("renderMarkdown", () => {
  it("marks generated item refs as trusted navigation targets", () => {
    const html = renderMarkdown("See #42", {
      owner: "acme",
      name: "widget",
      platformHost: "ghe.example.com",
    });

    const doc = new DOMParser().parseFromString(html, "text/html");
    const link = doc.querySelector("a.item-ref");
    expect(link?.getAttribute("data-middleman-item-ref")).toBe("true");
    expect(link?.getAttribute("data-owner")).toBe("acme");
    expect(link?.getAttribute("data-name")).toBe("widget");
    expect(link?.getAttribute("data-number")).toBe("42");
    expect(link?.getAttribute("data-platform-host")).toBe("ghe.example.com");
  });

  it("does not trust raw HTML that forges item ref attributes", () => {
    const html = renderMarkdown(`
<a class="item-ref"
  href="/pulls/acme/widget/42"
  data-middleman-item-ref="true"
  data-owner="acme"
  data-name="widget"
  data-number="42"
  data-platform-host="ghe.example.com">#42</a>
`);

    const doc = new DOMParser().parseFromString(html, "text/html");
    const link = doc.querySelector("a.item-ref");
    expect(link?.getAttribute("data-middleman-item-ref")).toBeNull();
    expect(link?.getAttribute("data-owner")).toBe("acme");
    expect(link?.getAttribute("data-name")).toBe("widget");
    expect(link?.getAttribute("data-number")).toBe("42");
    expect(link?.getAttribute("data-platform-host")).toBe("ghe.example.com");
  });
});
