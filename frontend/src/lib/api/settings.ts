import type { Settings } from "./types.js";

const basePath = (window.__BASE_PATH__ ?? "/").replace(/\/$/, "");
const BASE = `${basePath}/api/v1`;

export async function getSettings(): Promise<Settings> {
  const res = await fetch(`${BASE}/settings`);
  if (!res.ok) throw new Error(`GET /settings → ${res.status}`);
  return res.json() as Promise<Settings>;
}

export async function updateSettings(
  settings: { activity: Settings["activity"] },
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
): Promise<{ owner: string; name: string }> {
  const res = await fetch(`${BASE}/repos`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ owner, name }),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    throw new Error(text);
  }
  return res.json();
}

export async function removeRepo(
  owner: string,
  name: string,
): Promise<void> {
  const res = await fetch(
    `${BASE}/repos/${owner}/${name}`,
    { method: "DELETE", headers: { "Content-Type": "application/json" } },
  );
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    throw new Error(text);
  }
}
