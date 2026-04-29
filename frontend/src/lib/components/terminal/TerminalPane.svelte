<script lang="ts">
  import { onMount } from "svelte";
  import { getStores } from "@middleman/ui";
  import { Terminal } from "restty/xterm";
  import { workspaceTmuxWebSocketPath } from "../../api/workspace-runtime.js";

  interface TerminalPaneProps {
    workspaceId?: string;
    websocketPath?: string;
    reconnectOnExit?: boolean;
    active?: boolean;
    onExit?: (code: number) => void;
    // When the session is already exited at mount time, skip the
    // WebSocket connect — the server's attach endpoint returns 404
    // for non-running sessions, which would loop scheduleReconnect.
    initialStatus?: string;
  }

  const {
    workspaceId,
    websocketPath,
    reconnectOnExit = true,
    active = true,
    onExit,
    initialStatus,
  }: TerminalPaneProps = $props();
  const { settings: settingsStore } = getStores();

  const basePath = (window.__BASE_PATH__ ?? "/").replace(/\/$/, "");

  let containerEl: HTMLDivElement;
  let terminal: Terminal | null = $state(null);
  let ws: WebSocket | null = null;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let restartTimer: ReturnType<typeof setTimeout> | null = null;
  let reconnectDelay = 1000;
  let resizeObserver: ResizeObserver | null = null;
  let refreshFrame: number | null = null;
  let disposed = false;
  let exited = false;
  const encoder = new TextEncoder();
  const outputDecoder = new TextDecoder();

  const MAX_RECONNECT_DELAY = 30000;
  const FALLBACK_CELL_WIDTH = 8;
  const FALLBACK_CELL_HEIGHT = 18;

  function defaultTerminalFontFamily(): string {
    const rootFontFamily = getComputedStyle(
      document.documentElement,
    )
      .getPropertyValue("--font-mono")
      .trim();
    return rootFontFamily || "monospace";
  }

  const terminalFontFamily = $derived.by(() => {
    const configured = settingsStore
      .getTerminalFontFamily()
      .trim();
    return configured || defaultTerminalFontFamily();
  });

  function defaultWebsocketPath(): string {
    if (!workspaceId) return "";
    return workspaceTmuxWebSocketPath(workspaceId);
  }

  function appendSizeParams(
    url: string,
    cols: number,
    rows: number,
  ): string {
    const sep = url.includes("?") ? "&" : "?";
    return `${url}${sep}cols=${cols}&rows=${rows}`;
  }

  function buildWsUrl(
    cols: number,
    rows: number,
  ): string | null {
    const path = websocketPath ?? defaultWebsocketPath();
    if (!path) return null;

    const withSize = appendSizeParams(path, cols, rows);
    if (/^wss?:\/\//.test(withSize)) {
      return withSize;
    }
    const devUrl = buildDevApiWsUrl(withSize);
    if (devUrl) return devUrl;
    const proto = location.protocol === "https:" ? "wss" : "ws";
    return `${proto}://${location.host}${withBasePath(withSize)}`;
  }

  function withBasePath(path: string): string {
    const normalizedPath = path.startsWith("/") ? path : `/${path}`;
    if (!basePath) return normalizedPath;
    if (
      normalizedPath === basePath ||
      normalizedPath.startsWith(`${basePath}/`)
    ) {
      return normalizedPath;
    }
    return `${basePath}${normalizedPath}`;
  }

  function buildDevApiWsUrl(path: string): string | null {
    if (!import.meta.env.DEV) return null;
    const apiUrl = window.__MIDDLEMAN_DEV_API_URL__?.trim();
    if (!apiUrl || !path.startsWith("/api/")) return null;

    try {
      const base = new URL(apiUrl);
      const requested = new URL(path, "http://middleman.local");
      const basePath = base.pathname.replace(/\/$/, "");
      base.protocol = base.protocol === "https:" ? "wss:" : "ws:";
      base.pathname = `${basePath}${requested.pathname}`;
      base.search = requested.search;
      base.hash = "";
      return base.toString();
    } catch {
      return null;
    }
  }

  function sendResize(cols: number, rows: number): void {
    sendControl("resize", cols, rows);
  }

  function sendRefresh(cols: number, rows: number): void {
    sendControl("refresh", cols, rows);
  }

  function sendControl(
    type: "resize" | "refresh",
    cols: number,
    rows: number,
  ): void {
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type, cols, rows }));
    }
  }

  function normalizeTerminalInput(data: string): string {
    return data.replaceAll("\b", "\x7f");
  }

  function sendTerminalInput(data: string): void {
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(encoder.encode(normalizeTerminalInput(data)));
    }
  }

  function measureCellSize(): { width: number; height: number } {
    const canvas = document.createElement("canvas");
    const ctx = canvas.getContext("2d");
    if (!ctx) {
      return {
        width: FALLBACK_CELL_WIDTH,
        height: FALLBACK_CELL_HEIGHT,
      };
    }

    ctx.font = `14px ${terminalFontFamily}`;
    const metrics = ctx.measureText("W");
    const measuredHeight =
      metrics.actualBoundingBoxAscent +
      metrics.actualBoundingBoxDescent;
    return {
      width: metrics.width || FALLBACK_CELL_WIDTH,
      height: measuredHeight
        ? Math.ceil(measuredHeight * 1.35)
        : FALLBACK_CELL_HEIGHT,
    };
  }

  function fitTerminal(notify = true): void {
    if (!terminal || !containerEl) return;
    if (containerEl.clientWidth <= 0 || containerEl.clientHeight <= 0) {
      return;
    }

    const { width, height } = measureCellSize();
    const cols = Math.max(
      2,
      Math.floor(containerEl.clientWidth / width),
    );
    const rows = Math.max(
      1,
      Math.floor(containerEl.clientHeight / height),
    );

    if (cols === terminal.cols && rows === terminal.rows) return;
    terminal.resize(cols, rows);
    if (notify) sendResize(cols, rows);
  }

  function refreshVisibleTerminal(): void {
    if (!terminal) return;

    fitTerminal(false);
    sendRefresh(terminal.cols, terminal.rows);
  }

  function scheduleTerminalRefresh(): void {
    if (refreshFrame !== null) {
      cancelAnimationFrame(refreshFrame);
    }
    refreshFrame = requestAnimationFrame(() => {
      refreshFrame = null;
      refreshVisibleTerminal();
    });
  }

  function connect(): void {
    if (disposed || !terminal) return;

    const cols = terminal.cols;
    const rows = terminal.rows;
    const url = buildWsUrl(cols, rows);
    if (!url) return;
    const socket = new WebSocket(url);
    socket.binaryType = "arraybuffer";
    ws = socket;

    socket.onopen = () => {
      reconnectDelay = 1000;
      if (active) scheduleTerminalRefresh();
    };

    socket.onmessage = (ev: MessageEvent) => {
      if (!terminal) return;
      if (ev.data instanceof ArrayBuffer) {
        terminal.write(
          outputDecoder.decode(
            new Uint8Array(ev.data),
            { stream: true },
          ),
        );
      } else if (typeof ev.data === "string") {
        try {
          const msg = JSON.parse(ev.data) as {
            type: string;
            code?: number;
          };
          if (msg.type === "exited") {
            onExit?.(msg.code ?? 0);
            exited = true;
            if (reconnectOnExit) {
              terminal.write(
                "\r\n\x1b[90m[Process exited — reconnecting...]\x1b[0m\r\n",
              );
              scheduleSessionRestart();
            } else {
              terminal.write(
                "\r\n\x1b[90m[Process exited]\x1b[0m\r\n",
              );
            }
          }
        } catch {
          // Non-JSON text frame; ignore.
        }
      }
    };

    socket.onclose = () => {
      scheduleReconnect();
    };

    socket.onerror = () => {
      socket.close();
    };
  }

  function scheduleSessionRestart(): void {
    if (disposed) return;
    if (restartTimer) clearTimeout(restartTimer);
    restartTimer = setTimeout(() => {
      restartTimer = null;
      if (disposed) return;
      // Close stale socket so its onclose handler
      // cannot schedule a duplicate reconnect.
      if (ws) {
        ws.onclose = null;
        ws.onerror = null;
        ws.onmessage = null;
        ws.close();
        ws = null;
      }
      exited = false;
      reconnectDelay = 1000;
      connect();
    }, 2000);
  }

  function scheduleReconnect(): void {
    if (disposed || exited) return;
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      reconnectDelay = Math.min(
        reconnectDelay * 2,
        MAX_RECONNECT_DELAY,
      );
      connect();
    }, reconnectDelay);
  }

  function cleanup(): void {
    disposed = true;
    if (resizeObserver) {
      resizeObserver.disconnect();
      resizeObserver = null;
    }
    if (reconnectTimer !== null) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    if (restartTimer !== null) {
      clearTimeout(restartTimer);
      restartTimer = null;
    }
    if (refreshFrame !== null) {
      cancelAnimationFrame(refreshFrame);
      refreshFrame = null;
    }
    if (ws) {
      ws.onclose = null;
      ws.onerror = null;
      ws.onmessage = null;
      ws.close();
      ws = null;
    }
    if (terminal) {
      terminal.dispose();
      terminal = null;
    }
  }

  $effect(() => {
    if (!terminal) return;
    terminal.setOption("fontFamily", terminalFontFamily);
    fitTerminal();
  });

  $effect(() => {
    if (!terminal || !active) return;
    scheduleTerminalRefresh();
  });

  onMount(() => {
    let started = false;

    function start(): void {
      if (started || disposed) return;
      started = true;

      const term = new Terminal({
        theme: {
          background: "#0d1117",
          foreground: "#c9d1d9",
          cursor: "#58a6ff",
        },
        cursorBlink: true,
        fontFamily: terminalFontFamily,
        appOptions: {
          fontSize: 14,
        },
      });
      terminal = term;

      term.open(containerEl);
      fitTerminal(false);

      term.onData((data: string) => {
        sendTerminalInput(data);
      });

      resizeObserver = new ResizeObserver(() => {
        fitTerminal();
      });
      resizeObserver.observe(containerEl);

      if (initialStatus === "exited") {
        exited = true;
        term.write("\r\n\x1b[90m[Process exited]\x1b[0m\r\n");
        return;
      }
      connect();
    }

    // Waiting for fonts keeps the measured cell dimensions aligned
    // with the terminal canvas before we open the WebSocket.
    if (document.fonts && typeof document.fonts.ready?.then === "function") {
      void document.fonts.ready.then(start);
    } else {
      start();
    }

    return cleanup;
  });
</script>

<div class="terminal-container" bind:this={containerEl}></div>

<style>
  .terminal-container {
    position: relative;
    width: 100%;
    height: 100%;
    overflow: hidden;
    background: #0d1117;
  }
</style>
