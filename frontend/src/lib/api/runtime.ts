import { createAPIClient } from "./generated/client.js";

const basePath =
  typeof window !== "undefined" ? window.__BASE_PATH__ ?? "/" : "/";
const baseUrl = `${basePath.replace(/\/$/, "")}/api/v1`;

export const client = createAPIClient(baseUrl);
