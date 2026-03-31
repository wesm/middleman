import { describe, expect, it, vi } from "vitest";

import { createRuntimeClient } from "./runtime.js";

describe("runtime", () => {
  it("serializes activity type filters as comma-separated query params", async () => {
    let requestURL = "";
    const fetchMock = vi.fn(async (request: Request) => {
      requestURL = request.url;
      return new Response(JSON.stringify({ items: [], capped: false }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });

    const client = createRuntimeClient(fetchMock, "https://middleman.test/api/v1");
    await client.GET("/activity", {
      params: { query: { types: ["comment", "review"] } },
    });

    expect(requestURL).toContain("types=comment,review");
    expect(requestURL).not.toContain("types=comment&types=review");
  });
});
