import { cleanup, render, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const {
  ghosttyTerminalCtor,
  mockGhosttyInit,
  xtermOnDataHandlers,
  xtermTerminalCtor,
  xtermOpen,
} = vi.hoisted(() => ({
  ghosttyTerminalCtor: vi.fn(),
  mockGhosttyInit: vi.fn().mockResolvedValue(undefined),
  xtermOnDataHandlers: [] as Array<(data: string) => void>,
  xtermTerminalCtor: vi.fn(),
  xtermOpen: vi.fn(),
}));

let configuredRenderer: "xterm" | "ghostty-web" = "xterm";
let configuredFontFamily = "";
let mockSockets: MockWebSocket[] = [];

class MockWebSocket {
  static OPEN = 1;
  readyState = 1;
  binaryType = "arraybuffer";
  onopen: (() => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  sent: Array<string | ArrayBuffer | ArrayBufferView> = [];

  constructor(public url: string) {
    mockSockets.push(this);
  }
  send(data: string | ArrayBuffer | ArrayBufferView): void {
    this.sent.push(data);
  }
  close(): void {}
}

vi.mock("@middleman/ui", () => ({
  getStores: () => ({
    settings: {
      getTerminalFontFamily: () => configuredFontFamily,
      getTerminalRenderer: () => configuredRenderer,
    },
  }),
}));

vi.mock("@xterm/xterm", () => ({
  Terminal: vi.fn().mockImplementation((options) => {
    xtermTerminalCtor(options);
    return {
      cols: 80,
      rows: 24,
      options: { ...options },
      clearTextureAtlas: vi.fn(),
      dispose: vi.fn(),
      loadAddon: vi.fn(),
      onBinary: vi.fn(),
      onData: vi.fn((handler: (data: string) => void) => {
        xtermOnDataHandlers.push(handler);
        return { dispose: vi.fn() };
      }),
      open: xtermOpen,
      refresh: vi.fn(),
      write: vi.fn(),
    };
  }),
}));

vi.mock("@xterm/addon-fit", () => ({
  FitAddon: vi.fn().mockImplementation(() => ({
    fit: vi.fn(),
  })),
}));

vi.mock("@xterm/addon-webgl", () => ({
  WebglAddon: vi.fn().mockImplementation(() => ({
    dispose: vi.fn(),
    onContextLoss: vi.fn(),
  })),
}));

vi.mock("@xterm/xterm/css/xterm.css", () => ({}));

vi.mock("ghostty-web", () => ({
  init: (...args: []) => mockGhosttyInit(...args),
  FitAddon: vi.fn().mockImplementation(() => ({
    fit: vi.fn(),
  })),
  Terminal: vi.fn().mockImplementation((options) => {
    ghosttyTerminalCtor(options);
    return {
      cols: 80,
      rows: 24,
      options: { ...options },
      dispose: vi.fn(),
      loadAddon: vi.fn(),
      onData: vi.fn(),
      open: vi.fn(),
      write: vi.fn(),
    };
  }),
}));

import TerminalPane from "./TerminalPane.svelte";

describe("TerminalPane", () => {
  beforeEach(() => {
    configuredRenderer = "xterm";
    configuredFontFamily = "";
    ghosttyTerminalCtor.mockReset();
    mockGhosttyInit.mockClear();
    xtermTerminalCtor.mockReset();
    xtermOpen.mockReset();
    xtermOnDataHandlers.length = 0;
    mockSockets = [];

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

  it("uses xterm.js by default", async () => {
    render(TerminalPane, { props: { workspaceId: "ws-123" } });

    await waitFor(() => expect(xtermTerminalCtor).toHaveBeenCalled());

    expect(ghosttyTerminalCtor).not.toHaveBeenCalled();
    expect(mockGhosttyInit).not.toHaveBeenCalled();
  });

  it("uses ghostty-web when selected", async () => {
    configuredRenderer = "ghostty-web";

    render(TerminalPane, { props: { workspaceId: "ws-123" } });

    await waitFor(() => expect(ghosttyTerminalCtor).toHaveBeenCalled());

    expect(xtermTerminalCtor).not.toHaveBeenCalled();
    expect(mockGhosttyInit).toHaveBeenCalledTimes(1);
  });

  it("filters tiny tmux mouse drags before sending terminal input", async () => {
    render(TerminalPane, { props: { workspaceId: "ws-123" } });

    await waitFor(() => expect(xtermOnDataHandlers).toHaveLength(1));
    expect(mockSockets).toHaveLength(1);

    xtermOnDataHandlers[0]!("\x1b[<0;10;5M\x1b[<32;12;5M\x1b[<0;12;5m");

    expect(sentText(mockSockets[0]!, mockSockets[0]!.sent.length - 1)).toBe("\x1b[<0;10;5M\x1b[<0;12;5m");
  });

  it("does not update drag filter state while disconnected", async () => {
    render(TerminalPane, { props: { workspaceId: "ws-123" } });

    await waitFor(() => expect(xtermOnDataHandlers).toHaveLength(1));
    const socket = mockSockets[0]!;
    socket.readyState = 0;
    socket.sent = [];

    xtermOnDataHandlers[0]!("\x1b[<0;10;5M");
    socket.readyState = MockWebSocket.OPEN;
    xtermOnDataHandlers[0]!("\x1b[<32;12;5M");

    expect(sentText(socket, 0)).toBe("\x1b[<32;12;5M");
  });
});

function sentText(socket: MockWebSocket, index: number): string {
  const value = socket.sent[index];
  if (typeof value === "string") return value;
  if (value instanceof ArrayBuffer) {
    return new TextDecoder().decode(value);
  }
  return new TextDecoder().decode(value);
}
