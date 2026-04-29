import { cleanup, render, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  configuredFontFamily: "",
  mockDispose: vi.fn(),
  mockOnData: vi.fn(),
  mockOpen: vi.fn(),
  mockResize: vi.fn(),
  mockSetOption: vi.fn(),
  terminalCtor: vi.fn(),
  terminalWrite: vi.fn(),
}));

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
      getTerminalFontFamily: () => mocks.configuredFontFamily,
    },
  }),
}));

vi.mock("restty/xterm", () => ({
  Terminal: vi.fn().mockImplementation((options) => {
    mocks.terminalCtor(options);
    return {
      cols: 80,
      rows: 24,
      open: mocks.mockOpen,
      onData: mocks.mockOnData,
      dispose: mocks.mockDispose,
      write: mocks.terminalWrite,
      resize: mocks.mockResize,
      setOption: mocks.mockSetOption,
      options: { ...options },
    };
  }),
}));

import TerminalPane from "./TerminalPane.svelte";

describe("TerminalPane", () => {
  beforeEach(() => {
    mocks.configuredFontFamily = "";
    delete window.__BASE_PATH__;
    window.__MIDDLEMAN_DEV_API_URL__ = "http://127.0.0.1:8091";
    mocks.terminalCtor.mockReset();
    mocks.mockOpen.mockReset();
    mocks.mockOnData.mockReset();
    mocks.mockDispose.mockReset();
    mocks.mockResize.mockReset();
    mocks.mockSetOption.mockReset();
    mocks.terminalWrite.mockReset();
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
    vi.useRealTimers();
  });

  it("uses the configured settings font family for restty", async () => {
    mocks.configuredFontFamily = "\"Fira Code\", monospace";

    render(TerminalPane, {
      props: { workspaceId: "ws-123" },
    });

    await waitFor(() =>
      expect(mocks.terminalCtor).toHaveBeenCalledWith(
        expect.objectContaining({
          fontFamily: "\"Fira Code\", monospace",
        }),
      ),
    );
  });

  it("uses the /ws terminal route for the default workspace socket", async () => {
    render(TerminalPane, {
      props: { workspaceId: "ws-123" },
    });

    await waitFor(() => expect(sockets).toHaveLength(1));
    const url = new URL(socketAt(0).url);
    expect(url.origin).toBe("ws://localhost:3000");
    expect(url.pathname).toBe("/ws/v1/workspaces/ws-123/terminal");
  });

  it("applies the base path to the default workspace socket", async () => {
    window.__BASE_PATH__ = "/middleman/";

    render(TerminalPane, {
      props: { workspaceId: "ws-123" },
    });

    await waitFor(() => expect(sockets).toHaveLength(1));
    const url = new URL(socketAt(0).url);
    expect(url.origin).toBe("ws://localhost:3000");
    expect(url.pathname).toBe(
      "/middleman/ws/v1/workspaces/ws-123/terminal",
    );
  });

  it("connects to an explicit websocket path", async () => {
    render(TerminalPane, {
      props: {
        websocketPath:
          "/api/v1/workspaces/ws-123/runtime/sessions/ws-123%3Ahelper/terminal",
      },
    });

    await waitFor(() => expect(sockets).toHaveLength(1));
    const url = new URL(socketAt(0).url);
    expect(url.origin).toBe("ws://127.0.0.1:8091");
    expect(url.pathname).toBe(
      "/api/v1/workspaces/ws-123/runtime/sessions/ws-123%3Ahelper/terminal",
    );
    expect(url.searchParams.get("cols")).toBe("80");
    expect(url.searchParams.get("rows")).toBe("24");
  });

  it("keeps /ws paths on the current dev origin for Vite proxying", async () => {
    render(TerminalPane, {
      props: {
        websocketPath:
          "/ws/v1/workspaces/ws-123/runtime/sessions/ws-123%3Ahelper/terminal",
      },
    });

    await waitFor(() => expect(sockets).toHaveLength(1));
    const url = new URL(socketAt(0).url);
    expect(url.origin).toBe("ws://localhost:3000");
    expect(url.pathname).toBe(
      "/ws/v1/workspaces/ws-123/runtime/sessions/ws-123%3Ahelper/terminal",
    );
  });

  it("sends terminal input as binary and maps backspace to DEL", async () => {
    render(TerminalPane, {
      props: { workspaceId: "ws-123" },
    });

    await waitFor(() => expect(sockets).toHaveLength(1));
    const onData = mocks.mockOnData.mock.calls[0]?.[0] as
      | ((data: string) => void)
      | undefined;
    expect(onData).toBeDefined();

    onData?.("ab\b");

    const sent = socketAt(0).sent.at(-1);
    expect(ArrayBuffer.isView(sent)).toBe(true);
    expect(Array.from(sent as Uint8Array)).toEqual([
      97,
      98,
      0x7f,
    ]);
  });

  it("does not duplicate the base path for explicit websocket paths", async () => {
    window.__BASE_PATH__ = "/middleman/";

    render(TerminalPane, {
      props: {
        websocketPath:
          "/middleman/ws/v1/workspaces/ws-123/runtime/sessions/ws-123%3Ahelper/terminal",
      },
    });

    await waitFor(() => expect(sockets).toHaveLength(1));
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

    await waitFor(() => expect(sockets).toHaveLength(1));
    expect(mocks.mockResize).not.toHaveBeenCalled();
    expect(socketAt(0).sent).toEqual([]);

    await rerender({
      websocketPath:
        "/ws/v1/workspaces/ws-123/runtime/sessions/ws-123%3Ahelper/terminal",
      active: true,
    });

    expect(mocks.mockResize).not.toHaveBeenCalled();
    expect(socketAt(0).sent).toContain(
      JSON.stringify({ type: "refresh", cols: 80, rows: 24 }),
    );
  });

  it("does not open a websocket when initialStatus is exited", async () => {
    render(TerminalPane, {
      props: {
        websocketPath:
          "/api/v1/workspaces/ws-123/runtime/sessions/ws-123%3Ahelper/terminal",
        reconnectOnExit: false,
        initialStatus: "exited",
      },
    });

    await waitFor(() =>
      expect(mocks.terminalWrite).toHaveBeenCalledWith(
        expect.stringContaining("[Process exited]"),
      ),
    );
    expect(sockets).toHaveLength(0);
  });

  it("does not restart sessions when reconnectOnExit is false", async () => {
    const onExit = vi.fn();

    render(TerminalPane, {
      props: {
        websocketPath:
          "/api/v1/workspaces/ws-123/runtime/sessions/ws-123%3Ahelper/terminal",
        reconnectOnExit: false,
        onExit,
      },
    });

    await waitFor(() => expect(sockets).toHaveLength(1));
    vi.useFakeTimers();
    const socket = socketAt(0);
    socket.onmessage?.(
      new MessageEvent("message", {
        data: JSON.stringify({ type: "exited", code: 0 }),
      }),
    );
    socket.onclose?.();
    vi.advanceTimersByTime(30000);

    expect(sockets).toHaveLength(1);
    expect(mocks.terminalWrite).toHaveBeenCalledWith(
      expect.stringContaining("[Process exited]"),
    );
    expect(onExit).toHaveBeenCalledWith(0);
  });
});
