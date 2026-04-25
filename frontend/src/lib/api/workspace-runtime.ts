import type {
  LaunchTarget,
  RuntimeSession,
  WorkspaceRuntime,
} from "@middleman/ui/api/types";

export type WorkspaceRuntimeState = Omit<
  WorkspaceRuntime,
  "launch_targets" | "sessions"
> & {
  launch_targets: LaunchTarget[];
  sessions: RuntimeSession[];
};

export type RuntimeFetch = typeof fetch;

const basePath =
  typeof window !== "undefined" ? window.__BASE_PATH__ ?? "/" : "/";
const baseUrl = `${basePath.replace(/\/$/, "")}/api/v1`;

function workspaceRuntimeURL(workspaceId: string): string {
  return `${baseUrl}/workspaces/${encodeURIComponent(workspaceId)}/runtime`;
}

async function readJSON<T>(
  response: Response,
  fallback: string,
): Promise<T> {
  if (response.ok) {
    return (await response.json()) as T;
  }
  const body = await response.json().catch(() => ({})) as {
    detail?: string;
    title?: string;
  };
  throw new Error(body.detail ?? body.title ?? fallback);
}

export async function getWorkspaceRuntime(
  workspaceId: string,
  fetchFn: RuntimeFetch = fetch,
): Promise<WorkspaceRuntimeState> {
  const response = await fetchFn(workspaceRuntimeURL(workspaceId));
  const runtime = await readJSON<WorkspaceRuntime>(
    response,
    `GET workspace runtime failed (${response.status})`,
  );
  return {
    ...runtime,
    launch_targets: runtime.launch_targets ?? [],
    sessions: runtime.sessions ?? [],
  };
}

export async function launchWorkspaceSession(
  workspaceId: string,
  targetKey: string,
  fetchFn: RuntimeFetch = fetch,
): Promise<RuntimeSession> {
  const response = await fetchFn(
    `${workspaceRuntimeURL(workspaceId)}/sessions`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ target_key: targetKey }),
    },
  );
  return readJSON<RuntimeSession>(
    response,
    `Launch session failed (${response.status})`,
  );
}

export async function stopWorkspaceSession(
  workspaceId: string,
  sessionKey: string,
  fetchFn: RuntimeFetch = fetch,
): Promise<void> {
  const response = await fetchFn(
    `${workspaceRuntimeURL(workspaceId)}/sessions/${encodeURIComponent(sessionKey)}`,
    {
      method: "DELETE",
      headers: { "Content-Type": "application/json" },
    },
  );
  if (!response.ok && response.status !== 204) {
    await readJSON<unknown>(
      response,
      `Stop session failed (${response.status})`,
    );
  }
}

export async function ensureWorkspaceShell(
  workspaceId: string,
  fetchFn: RuntimeFetch = fetch,
): Promise<RuntimeSession> {
  const response = await fetchFn(
    `${workspaceRuntimeURL(workspaceId)}/shell`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
    },
  );
  return readJSON<RuntimeSession>(
    response,
    `Ensure shell failed (${response.status})`,
  );
}

export function workspaceSessionWebSocketPath(
  workspaceId: string,
  sessionKey: string,
): string {
  return (
    `/api/v1/workspaces/${encodeURIComponent(workspaceId)}` +
    `/runtime/sessions/${encodeURIComponent(sessionKey)}` +
    "/terminal"
  );
}

export function workspaceShellWebSocketPath(
  workspaceId: string,
): string {
  return (
    `/api/v1/workspaces/${encodeURIComponent(workspaceId)}` +
    "/runtime/shell/terminal"
  );
}

export function workspaceTmuxWebSocketPath(
  workspaceId: string,
): string {
  return (
    `/api/v1/workspaces/${encodeURIComponent(workspaceId)}` +
    "/terminal"
  );
}
