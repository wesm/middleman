import { cleanup, render } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mockFit = vi.fn();
const mockOpen = vi.fn();
const mockLoadAddon = vi.fn();
const mockOnData = vi.fn();
const mockOnBinary = vi.fn();
const mockDispose = vi.fn();
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

  constructor(public url: string) {
    sockets.push(this);
  }

  send(): void {}
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
    window.__MIDDLEMAN_DEV_API_URL__ = "http://127.0.0.1:8091";
    terminalCtor.mockReset();
    mockFit.mockReset();
    mockOpen.mockReset();
    mockLoadAddon.mockReset();
    mockOnData.mockReset();
    mockOnBinary.mockReset();
    mockDispose.mockReset();
    terminalWrite.mockReset();
    sockets = [];

    vi.stubGlobal("ResizeObserver", class {
      observe(): void {}
      disconnect(): void {}
    });

    vi.stubGlobal("WebSocket", MockWebSocket);
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
