/**
 * This file was auto-generated from frontend/openapi/openapi.json.
 * Do not make direct changes to the file.
 */

import createClient from "openapi-fetch";
import type { paths } from "./schema";

export function createAPIClient(baseUrl: string) {
  return createClient<paths>({ baseUrl });
}
