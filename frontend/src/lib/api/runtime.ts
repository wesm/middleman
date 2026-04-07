import type { QuerySerializerOptions } from "openapi-fetch";

import { createAPIClient } from "@middleman/ui/api/client";
import type { components } from "@middleman/ui/api/schema";
import { csrfFetch, type FetchFn } from "@middleman/ui/api/csrf";

const basePath =
  typeof window !== "undefined" ? window.__BASE_PATH__ ?? "/" : "/";
const baseUrl = `${basePath.replace(/\/$/, "")}/api/v1`;

export const querySerializer: QuerySerializerOptions = {
  array: {
    style: "form",
    explode: false,
  },
};

export function createRuntimeClient(
  fetch?: FetchFn,
  clientBaseURL = baseUrl,
) {
  const inner = fetch ?? globalThis.fetch.bind(globalThis);
  return createAPIClient(clientBaseURL, {
    fetch: csrfFetch(inner),
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
