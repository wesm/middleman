/**
 * This file was auto-generated from frontend/openapi/openapi.json.
 * Do not make direct changes to the file.
 */

import createClient, { type ClientOptions } from "openapi-fetch";
import type { paths } from "./schema";

export function createAPIClient(baseUrl: string, options: Pick<ClientOptions, "fetch" | "querySerializer"> = {}) {
  return createClient<paths>({ baseUrl, ...options });
}
