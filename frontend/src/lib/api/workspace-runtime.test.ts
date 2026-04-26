import { describe, expect, it, vi } from "vitest";

import {
  ensureWorkspaceShell,
  getWorkspaceRuntime,
  launchWorkspaceSession,
  stopWorkspaceSession,
  workspaceSessionWebSocketPath,
  workspaceShellWebSocketPath,
  workspaceTmuxWebSocketPath,
} from "./workspace-runtime.js";

describe("workspace-runtime api", () => {
  it("loads runtime state and normalizes nullable arrays", async () => {
    const fetchMock = vi.fn(async () =>
      new Response(
        JSON.stringify({
          launch_targets: null,
          sessions: null,
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
    );

    const runtime = await getWorkspaceRuntime("ws-1", fetchMock);

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/workspaces/ws-1/runtime",
    );
    expect(runtime.launch_targets).toEqual([]);
    expect(runtime.sessions).toEqual([]);
  });

  it("launches and stops sessions with JSON mutation requests", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            key: "ws-1:helper",
            workspace_id: "ws-1",
            target_key: "helper",
            label: "Helper",
            kind: "agent",
            status: "running",
            created_at: "2026-04-25T00:00:00Z",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        ),
      )
      .mockResolvedValueOnce(new Response(null, { status: 204 }));

    await launchWorkspaceSession("ws-1", "helper", fetchMock);
    await stopWorkspaceSession("ws-1", "ws-1:helper", fetchMock);

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/v1/workspaces/ws-1/runtime/sessions",
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ target_key: "helper" }),
      },
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/v1/workspaces/ws-1/runtime/sessions/ws-1%3Ahelper",
      {
        method: "DELETE",
        headers: { "Content-Type": "application/json" },
      },
    );
  });

  it("ensures shell and builds websocket paths", async () => {
    const fetchMock = vi.fn(async () =>
      new Response(
        JSON.stringify({
          key: "ws-1:shell",
          workspace_id: "ws-1",
          target_key: "plain_shell",
          label: "Shell",
          kind: "plain_shell",
          status: "running",
          created_at: "2026-04-25T00:00:00Z",
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
    );

    await ensureWorkspaceShell("ws-1", fetchMock);

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/workspaces/ws-1/runtime/shell",
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
      },
    );
    expect(
      workspaceSessionWebSocketPath("ws-1", "ws-1:helper"),
    ).toBe(
      "/api/v1/workspaces/ws-1/runtime/sessions/ws-1%3Ahelper/terminal",
    );
    expect(workspaceShellWebSocketPath("ws-1")).toBe(
      "/api/v1/workspaces/ws-1/runtime/shell/terminal",
    );
    expect(workspaceTmuxWebSocketPath("ws-1")).toBe(
      "/api/v1/workspaces/ws-1/terminal",
    );
  });
});
