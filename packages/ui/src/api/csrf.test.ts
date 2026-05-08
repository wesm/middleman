import { describe, expect, it, vi } from "vitest";

import { csrfFetch } from "./csrf.js";

describe("csrfFetch", () => {
  it("forwards fetch init options when called with a URL", async () => {
    let request: Request | null = null;
    const inner = vi.fn(async (input: RequestInfo | URL) => {
      request = input instanceof Request ? input : new Request(input);
      return Response.json({});
    });

    const fetch = csrfFetch(inner);
    await fetch("https://middleman.test/api/v1/settings", {
      method: "POST",
      body: JSON.stringify({ theme: "dark" }),
      headers: { "X-Test": "present" },
    });

    expect(request?.url).toBe("https://middleman.test/api/v1/settings");
    expect(request?.method).toBe("POST");
    expect(request?.headers.get("X-Test")).toBe("present");
    expect(request?.headers.get("Content-Type")).toBe("application/json");
    await expect(request?.text()).resolves.toBe('{"theme":"dark"}');
  });

  it("does not overwrite generated multipart content types", async () => {
    let request: Request | null = null;
    const inner = vi.fn(async (input: RequestInfo | URL) => {
      request = input instanceof Request ? input : new Request(input);
      return Response.json({});
    });

    const body = new FormData();
    body.append("upload", new Blob(["avatar"]), "avatar.txt");

    const fetch = csrfFetch(inner);
    await fetch("https://middleman.test/api/v1/uploads", { method: "POST", body });

    expect(request?.headers.get("Content-Type")).not.toBe("application/json");
  });

  it("does not overwrite generated form content types", async () => {
    let request: Request | null = null;
    const inner = vi.fn(async (input: RequestInfo | URL) => {
      request = input instanceof Request ? input : new Request(input);
      return Response.json({});
    });

    const fetch = csrfFetch(inner);
    await fetch("https://middleman.test/api/v1/search", {
      method: "POST",
      body: new URLSearchParams({ q: "notifications" }),
    });

    expect(request?.headers.get("Content-Type")).not.toBe("application/json");
    await expect(request?.text()).resolves.toBe("q=notifications");
  });

  it("replaces Request text/plain content types on generated JSON mutations", async () => {
    let request: Request | null = null;
    const inner = vi.fn(async (input: RequestInfo | URL) => {
      request = input instanceof Request ? input : new Request(input);
      return Response.json({});
    });

    const fetch = csrfFetch(inner);
    await fetch(new Request("https://middleman.test/api/v1/ready", {
      method: "POST",
      body: JSON.stringify({ ready: true }),
    }));

    expect(request?.headers.get("Content-Type")).toBe("application/json");
    await expect(request?.json()).resolves.toEqual({ ready: true });
  });

  it("adds JSON content type to zero-body mutation requests", async () => {
    let request: Request | null = null;
    const inner = vi.fn(async (input: RequestInfo | URL) => {
      request = input instanceof Request ? input : new Request(input);
      return Response.json({});
    });

    const fetch = csrfFetch(inner);
    await fetch("https://middleman.test/api/v1/notifications/sync", { method: "POST" });

    expect(request?.method).toBe("POST");
    expect(request?.headers.get("Content-Type")).toBe("application/json");
    await expect(request?.text()).resolves.toBe("");
  });
});
