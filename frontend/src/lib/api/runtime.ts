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

type FetchFn = NonNullable<ClientOptions["fetch"]>;

// Wraps fetch to ensure Content-Type: application/json on all
// mutation requests, required by the server's CSRF protection.
function csrfFetch(inner: FetchFn): FetchFn {
  return (input: Request) => {
    const method = input.method.toUpperCase();
    if (method !== "GET" && method !== "HEAD") {
      if (!input.headers.has("Content-Type")) {
        const headers = new Headers(input.headers);
        headers.set("Content-Type", "application/json");
        return inner(new Request(input, { headers }));
      }
    }
    return inner(input);
  };
}

export function createRuntimeClient(
  fetch?: FetchFn,
  clientBaseURL = baseUrl,
) {
  const inner = fetch ?? globalThis.fetch.bind(globalThis);
  return createAPIClient(clientBaseURL, {
    fetch: csrfFetch(inner as FetchFn),
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
