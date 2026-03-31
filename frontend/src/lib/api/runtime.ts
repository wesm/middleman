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

export function createRuntimeClient(
  fetch?: ClientOptions["fetch"],
  clientBaseURL = baseUrl,
) {
  return createAPIClient(clientBaseURL, { fetch, querySerializer });
}

export const client = createRuntimeClient();

export function apiErrorMessage(
  error: components["schemas"]["ErrorModel"] | undefined,
  fallback: string,
): string {
  return error?.detail ?? error?.title ?? fallback;
}
