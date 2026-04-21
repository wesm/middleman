import { cleanup, render } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mockFit = vi.fn();
const mockOpen = vi.fn();
const mockLoadAddon = vi.fn();
const mockOnData = vi.fn();
const mockOnBinary = vi.fn();
const mockDispose = vi.fn();
const terminalCtor = vi.fn();

let configuredFontFamily = "";

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
      write: vi.fn(),
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
    terminalCtor.mockReset();
    mockFit.mockReset();
    mockOpen.mockReset();
    mockLoadAddon.mockReset();
    mockOnData.mockReset();
    mockOnBinary.mockReset();
    mockDispose.mockReset();

    vi.stubGlobal("ResizeObserver", class {
      observe(): void {}
      disconnect(): void {}
    });

    vi.stubGlobal("WebSocket", class {
      static OPEN = 1;
      readyState = 1;
      binaryType = "arraybuffer";
      onopen: (() => void) | null = null;
      onmessage: ((event: MessageEvent) => void) | null = null;
      onclose: (() => void) | null = null;
      onerror: (() => void) | null = null;

      constructor(_: string) {}

      send(): void {}
      close(): void {}
    });
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
});
