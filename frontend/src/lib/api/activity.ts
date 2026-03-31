const basePath = (import.meta.env.BASE_URL ?? "/").replace(/\/$/, "");
const BASE = `${basePath}/api/v1`;

export interface ActivityItem {
  id: string;
  cursor: string;
  activity_type: "new_pr" | "new_issue" | "comment" | "review" | "commit";
  repo_owner: string;
  repo_name: string;
  item_type: "pr" | "issue";
  item_number: number;
  item_title: string;
  item_url: string;
  item_state: "open" | "merged" | "closed";
  author: string;
  created_at: string;
  body_preview: string;
}

export interface ActivityResponse {
  items: ActivityItem[];
  capped: boolean;
}

export interface ActivityParams {
  repo?: string;
  types?: string[];
  search?: string;
  since?: string;
  after?: string;
}

export async function listActivity(params?: ActivityParams): Promise<ActivityResponse> {
  const sp = new URLSearchParams();
  if (params?.repo) sp.set("repo", params.repo);
  if (params?.types && params.types.length > 0) sp.set("types", params.types.join(","));
  if (params?.search) sp.set("search", params.search);
  if (params?.since) sp.set("since", params.since);
  if (params?.after) sp.set("after", params.after);
  const qs = sp.toString();
  const res = await fetch(`${BASE}/activity${qs ? `?${qs}` : ""}`);
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    throw new Error(`GET /activity → ${res.status}: ${text}`);
  }
  return res.json() as Promise<ActivityResponse>;
}
