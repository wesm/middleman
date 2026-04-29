import type { QuerySerializerOptions } from "openapi-fetch";

import { createAPIClient } from "@middleman/ui/api/client";
import type { components } from "@middleman/ui/api/schema";
import { csrfFetch, type FetchFn } from "@middleman/ui/api/csrf";

const basePath =
  typeof window !== "undefined" ? window.__BASE_PATH__ ?? "/" : "/";
const baseUrl =
  typeof window !== "undefined"
    ? new URL(
        `${basePath.replace(/\/$/, "")}/api/v1`,
        window.location.origin,
      ).toString()
    : "http://localhost/api/v1";

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
  error:
    | Pick<
        Partial<components["schemas"]["ErrorModel"]>,
        "detail" | "title"
      >
    | undefined,
  fallback: string,
): string {
  return error?.detail ?? error?.title ?? fallback;
}
