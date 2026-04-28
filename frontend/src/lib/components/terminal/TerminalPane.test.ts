import { cleanup, render } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mockFit = vi.fn();
const mockOpen = vi.fn();
const mockLoadAddon = vi.fn();
const mockOnData = vi.fn();
const mockOnBinary = vi.fn();
const mockDispose = vi.fn();
const mockRefresh = vi.fn();
const mockClearTextureAtlas = vi.fn();
const terminalCtor = vi.fn();
const terminalWrite = vi.fn();

let configuredFontFamily = "";
let sockets: MockWebSocket[] = [];

class MockWebSocket {
  static OPEN = 1;
  readyState = 1;
  binaryType = "arraybuffer";
  onopen: (() => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  sent: unknown[] = [];

  constructor(public url: string) {
    sockets.push(this);
  }

  send(data: unknown): void {
    this.sent.push(data);
  }
  close(): void {}
}

function socketAt(index: number): MockWebSocket {
  const socket = sockets[index];
  expect(socket).toBeDefined();
  return socket!;
}

vi.mock("@middleman/ui", () => ({
  getStores: () => ({
    settings: {
      getTerminalFontFamily: () => configuredFontFamily,
    },
  }),
}));

vi.mock("@xterm/xterm", () => ({
  Terminal: vi.fn().mockImplementation((options) => {
    terminalCtor(options);
    return {
      cols: 80,
      rows: 24,
      open: mockOpen,
      loadAddon: mockLoadAddon,
      onData: mockOnData,
      onBinary: mockOnBinary,
      dispose: mockDispose,
      write: terminalWrite,
      refresh: mockRefresh,
      clearTextureAtlas: mockClearTextureAtlas,
      options: { ...options },
    };
  }),
}));

vi.mock("@xterm/addon-fit", () => ({
  FitAddon: vi.fn().mockImplementation(() => ({
    fit: mockFit,
  })),
}));

vi.mock("@xterm/addon-webgl", () => ({
  WebglAddon: vi.fn().mockImplementation(() => ({})),
}));

import TerminalPane from "./TerminalPane.svelte";

describe("TerminalPane", () => {
  beforeEach(() => {
    configuredFontFamily = "";
    delete window.__BASE_PATH__;
    window.__MIDDLEMAN_DEV_API_URL__ = "http://127.0.0.1:8091";
    terminalCtor.mockReset();
    mockFit.mockReset();
    mockOpen.mockReset();
    mockLoadAddon.mockReset();
    mockOnData.mockReset();
    mockOnBinary.mockReset();
    mockDispose.mockReset();
    mockRefresh.mockReset();
    mockClearTextureAtlas.mockReset();
    terminalWrite.mockReset();
    sockets = [];

    vi.stubGlobal("ResizeObserver", class {
      observe(): void {}
      disconnect(): void {}
    });

    vi.stubGlobal("WebSocket", MockWebSocket);
    vi.stubGlobal(
      "requestAnimationFrame",
      (callback: FrameRequestCallback) => {
        callback(0);
        return 1;
      },
    );
    vi.stubGlobal("cancelAnimationFrame", () => undefined);
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("uses the configured settings font family for xterm", () => {
    configuredFontFamily = "\"Fira Code\", monospace";

    render(TerminalPane, {
      props: { workspaceId: "ws-123" },
    });

    expect(terminalCtor).toHaveBeenCalledWith(
      expect.objectContaining({
        fontFamily: "\"Fira Code\", monospace",
      }),
    );
  });

  it("uses the /ws terminal route for the default workspace socket", () => {
    render(TerminalPane, {
      props: { workspaceId: "ws-123" },
    });

    expect(sockets).toHaveLength(1);
    const url = new URL(socketAt(0).url);
    expect(url.origin).toBe("ws://localhost:3000");
    expect(url.pathname).toBe("/ws/v1/workspaces/ws-123/terminal");
  });

  it("applies the base path to the default workspace socket", () => {
    window.__BASE_PATH__ = "/middleman/";

    render(TerminalPane, {
      props: { workspaceId: "ws-123" },
    });

    expect(sockets).toHaveLength(1);
    const url = new URL(socketAt(0).url);
    expect(url.origin).toBe("ws://localhost:3000");
    expect(url.pathname).toBe(
      "/middleman/ws/v1/workspaces/ws-123/terminal",
    );
  });

  it("connects to an explicit websocket path", () => {
    render(TerminalPane, {
      props: {
        websocketPath:
          "/api/v1/workspaces/ws-123/runtime/sessions/ws-123%3Ahelper/terminal",
      },
    });

    expect(sockets).toHaveLength(1);
    const url = new URL(socketAt(0).url);
    expect(url.origin).toBe("ws://127.0.0.1:8091");
    expect(url.pathname).toBe(
      "/api/v1/workspaces/ws-123/runtime/sessions/ws-123%3Ahelper/terminal",
    );
    expect(url.searchParams.get("cols")).toBe("80");
    expect(url.searchParams.get("rows")).toBe("24");
  });

  it("keeps /ws paths on the current dev origin for Vite proxying", () => {
    render(TerminalPane, {
      props: {
        websocketPath:
          "/ws/v1/workspaces/ws-123/runtime/sessions/ws-123%3Ahelper/terminal",
      },
    });

    expect(sockets).toHaveLength(1);
    const url = new URL(socketAt(0).url);
    expect(url.origin).toBe("ws://localhost:3000");
    expect(url.pathname).toBe(
      "/ws/v1/workspaces/ws-123/runtime/sessions/ws-123%3Ahelper/terminal",
    );
  });

  it("does not duplicate the base path for explicit websocket paths", () => {
    window.__BASE_PATH__ = "/middleman/";

    render(TerminalPane, {
      props: {
        websocketPath:
          "/middleman/ws/v1/workspaces/ws-123/runtime/sessions/ws-123%3Ahelper/terminal",
      },
    });

    expect(sockets).toHaveLength(1);
    const url = new URL(socketAt(0).url);
    expect(url.origin).toBe("ws://localhost:3000");
    expect(url.pathname).toBe(
      "/middleman/ws/v1/workspaces/ws-123/runtime/sessions/ws-123%3Ahelper/terminal",
    );
  });

  it("refreshes the terminal when a hidden pane becomes active", async () => {
    const { rerender } = render(TerminalPane, {
      props: {
        websocketPath:
          "/ws/v1/workspaces/ws-123/runtime/sessions/ws-123%3Ahelper/terminal",
        active: false,
      },
    });

    expect(mockRefresh).not.toHaveBeenCalled();
    expect(socketAt(0).sent).toEqual([]);

    await rerender({
      websocketPath:
        "/ws/v1/workspaces/ws-123/runtime/sessions/ws-123%3Ahelper/terminal",
      active: true,
    });

    expect(mockFit).toHaveBeenCalled();
    expect(mockClearTextureAtlas).toHaveBeenCalled();
    expect(mockRefresh).toHaveBeenCalledWith(0, 23);
    expect(socketAt(0).sent).toContain(
      JSON.stringify({ type: "refresh", cols: 80, rows: 24 }),
    );
  });

  it("does not open a websocket when initialStatus is exited", () => {
    render(TerminalPane, {
      props: {
        websocketPath:
          "/api/v1/workspaces/ws-123/runtime/sessions/ws-123%3Ahelper/terminal",
        reconnectOnExit: false,
        initialStatus: "exited",
      },
    });

    expect(sockets).toHaveLength(0);
    expect(terminalWrite).toHaveBeenCalledWith(
      expect.stringContaining("[Process exited]"),
    );
  });

  it("does not restart sessions when reconnectOnExit is false", () => {
    vi.useFakeTimers();
    const onExit = vi.fn();

    render(TerminalPane, {
      props: {
        websocketPath:
          "/api/v1/workspaces/ws-123/runtime/sessions/ws-123%3Ahelper/terminal",
        reconnectOnExit: false,
        onExit,
      },
    });

    expect(sockets).toHaveLength(1);
    const socket = socketAt(0);
    socket.onmessage?.(
      new MessageEvent("message", {
        data: JSON.stringify({ type: "exited", code: 0 }),
      }),
    );
    socket.onclose?.();
    vi.advanceTimersByTime(30000);

    expect(sockets).toHaveLength(1);
    expect(terminalWrite).toHaveBeenCalledWith(
      expect.stringContaining("[Process exited]"),
    );
    expect(onExit).toHaveBeenCalledWith(0);

    vi.useRealTimers();
  });
});
