import type { ClientOptions, QuerySerializerOptions } from "openapi-fetch";

import { createAPIClient } from "./generated/client.js";
import type { components } from "./generated/schema.js";

const basePath =
  typeof window !== "undefined" ? window.__BASE_PATH__ ?? "/" : "/";
const baseUrl = `${basePath.replace(/\/$/, "")}/api/v1`;

export const querySerializer: QuerySerializerOptions = {
  array: {
    style: "form",
    explode: false,
  },
};

// Wraps fetch to ensure Content-Type: application/json on all
// mutation requests, required by the server's CSRF protection.
function csrfFetch(
  inner: typeof globalThis.fetch = globalThis.fetch,
): typeof globalThis.fetch {
  return (input, init) => {
    const method = init?.method?.toUpperCase() ?? "GET";
    if (method !== "GET" && method !== "HEAD") {
      const headers = new Headers(init?.headers);
      if (!headers.has("Content-Type")) {
        headers.set("Content-Type", "application/json");
      }
      return inner(input, { ...init, headers });
    }
    return inner(input, init);
  };
}

export function createRuntimeClient(
  fetch?: ClientOptions["fetch"],
  clientBaseURL = baseUrl,
) {
  return createAPIClient(clientBaseURL, {
    fetch: csrfFetch(fetch ?? globalThis.fetch),
    querySerializer,
  });
}

export const client = createRuntimeClient();

export function apiErrorMessage(
  error: components["schemas"]["ErrorModel"] | undefined,
  fallback: string,
): string {
  return error?.detail ?? error?.title ?? fallback;
}
