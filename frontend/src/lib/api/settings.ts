import type { Settings } from "@middleman/ui/api/types";

const basePath = ((globalThis as typeof globalThis & {
  window?: { __BASE_PATH__?: string };
}).window?.__BASE_PATH__ ?? "/").replace(/\/$/, "");
const BASE = `${basePath}/api/v1`;

export interface RepoPreviewRow {
  owner: string;
  name: string;
  description: string | null;
  private: boolean;
  pushed_at: string | null;
  already_configured: boolean;
}

export interface RepoPreviewResponse {
  owner: string;
  pattern: string;
  repos: RepoPreviewRow[];
}

interface RepoInput {
  owner: string;
  name: string;
}

async function errorFromResponse(res: Response, fallback: string): Promise<Error> {
  const cloned = res.clone();
  try {
    const data = await res.json() as { error?: string; detail?: string; title?: string };
    if (data.error) return new Error(data.error);
    if (data.detail) return new Error(data.detail);
    if (data.title) return new Error(data.title);
  } catch {
    // Fall through to text fallback.
  }
  const text = await cloned.text().catch(() => res.statusText);
  return new Error(text || fallback);
}

export async function getSettings(): Promise<Settings> {
  const res = await fetch(`${BASE}/settings`);
  if (!res.ok) throw new Error(`GET /settings → ${res.status}`);
  return res.json() as Promise<Settings>;
}

export async function updateSettings(
  settings: {
    activity?: Settings["activity"];
    terminal?: Settings["terminal"];
    agents?: Settings["agents"];
  },
): Promise<Settings> {
  const res = await fetch(`${BASE}/settings`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(settings),
  });
  if (!res.ok) throw new Error(`PUT /settings → ${res.status}`);
  return res.json() as Promise<Settings>;
}

export async function addRepo(
  owner: string,
  name: string,
): Promise<Settings> {
  const res = await fetch(`${BASE}/repos`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ owner, name }),
  });
  if (!res.ok) throw await errorFromResponse(res, `POST /repos → ${res.status}`);
  return res.json() as Promise<Settings>;
}

export async function removeRepo(
  owner: string,
  name: string,
): Promise<void> {
  const res = await fetch(
    `${BASE}/repos/${encodeURIComponent(owner)}/${encodeURIComponent(name)}`,
    { method: "DELETE", headers: { "Content-Type": "application/json" } },
  );
  if (!res.ok) throw await errorFromResponse(res, `DELETE /repos → ${res.status}`);
}

export async function refreshRepo(
  owner: string,
  name: string,
): Promise<Settings> {
  const res = await fetch(
    `${BASE}/repos/${encodeURIComponent(owner)}/${encodeURIComponent(name)}/refresh`,
    { method: "POST", headers: { "Content-Type": "application/json" } },
  );
  if (!res.ok) throw await errorFromResponse(res, `POST /repos/refresh → ${res.status}`);
  return res.json() as Promise<Settings>;
}

export async function previewRepos(
  owner: string,
  pattern: string,
): Promise<RepoPreviewResponse> {
  const res = await fetch(`${BASE}/repos/preview`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ owner, pattern }),
  });
  if (!res.ok) throw await errorFromResponse(res, `POST /repos/preview → ${res.status}`);
  return res.json() as Promise<RepoPreviewResponse>;
}

export async function bulkAddRepos(repos: RepoInput[]): Promise<Settings> {
  const res = await fetch(`${BASE}/repos/bulk`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ repos }),
  });
  if (!res.ok) throw await errorFromResponse(res, `POST /repos/bulk → ${res.status}`);
  return res.json() as Promise<Settings>;
}
