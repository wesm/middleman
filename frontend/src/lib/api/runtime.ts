import { createAPIClient } from "./generated/client.js";
import type { components } from "./generated/schema.js";

const basePath =
  typeof window !== "undefined" ? window.__BASE_PATH__ ?? "/" : "/";
const baseUrl = `${basePath.replace(/\/$/, "")}/api/v1`;

export const client = createAPIClient(baseUrl);

export function apiErrorMessage(
  error: components["schemas"]["ErrorModel"] | undefined,
  fallback: string,
): string {
  return error?.detail ?? error?.title ?? fallback;
}
