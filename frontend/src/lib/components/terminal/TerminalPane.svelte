<script lang="ts">
  import { onMount } from "svelte";
  import { getStores } from "@middleman/ui";
  import { Terminal } from "@xterm/xterm";
  import { FitAddon } from "@xterm/addon-fit";
  import { WebglAddon } from "@xterm/addon-webgl";
  import "@xterm/xterm/css/xterm.css";

  interface TerminalPaneProps {
    workspaceId?: string;
    websocketPath?: string;
    reconnectOnExit?: boolean;
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
    onExit,
    initialStatus,
  }: TerminalPaneProps = $props();
  const { settings: settingsStore } = getStores();

  const basePath = (window.__BASE_PATH__ ?? "/").replace(/\/$/, "");

  let containerEl: HTMLDivElement;
  let terminal: Terminal | null = $state(null);
  let fitAddon: FitAddon | null = null;
  let webglAddon: WebglAddon | null = null;
  let ws: WebSocket | null = null;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let restartTimer: ReturnType<typeof setTimeout> | null = null;
  let reconnectDelay = 1000;
  let resizeObserver: ResizeObserver | null = null;
  let disposed = false;
  let exited = false;
  const encoder = new TextEncoder();

  const MAX_RECONNECT_DELAY = 30000;

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
    return (
      `/ws/v1/workspaces/${encodeURIComponent(workspaceId)}` +
      "/terminal"
    );
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
    const normalizedPath = withSize.startsWith("/")
      ? withSize
      : `/${withSize}`;
    return (
      `${proto}://${location.host}${basePath}` +
      normalizedPath
    );
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
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: "resize", cols, rows }));
    }
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
    };

    socket.onmessage = (ev: MessageEvent) => {
      if (!terminal) return;
      if (ev.data instanceof ArrayBuffer) {
        terminal.write(new Uint8Array(ev.data));
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
    terminal.options.fontFamily = terminalFontFamily;
    // The WebGL renderer caches glyphs per font; force a rebuild so
    // cell widths and glyph metrics line up after the family changes.
    webglAddon?.clearTextureAtlas();
    fitAddon?.fit();
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
        fontSize: 14,
      });
      terminal = term;

      term.open(containerEl);

      const fit = new FitAddon();
      fitAddon = fit;
      term.loadAddon(fit);

      try {
        const wgl = new WebglAddon();
        wgl.onContextLoss(() => {
          wgl.dispose();
          if (webglAddon === wgl) webglAddon = null;
        });
        term.loadAddon(wgl);
        webglAddon = wgl;
      } catch {
        // WebGL unavailable; canvas renderer used as fallback.
      }

      fit.fit();

      term.onData((data: string) => {
        if (ws?.readyState === WebSocket.OPEN) {
          ws.send(encoder.encode(data));
        }
      });

      term.onBinary((data: string) => {
        if (ws?.readyState === WebSocket.OPEN) {
          const buf = new Uint8Array(data.length);
          for (let i = 0; i < data.length; i++) {
            buf[i] = data.charCodeAt(i) & 0xff;
          }
          ws.send(buf.buffer);
        }
      });

      resizeObserver = new ResizeObserver(() => {
        if (!fitAddon || !terminal) return;
        fitAddon.fit();
        sendResize(terminal.cols, terminal.rows);
      });
      resizeObserver.observe(containerEl);

      if (initialStatus === "exited") {
        exited = true;
        term.write("\r\n\x1b[90m[Process exited]\x1b[0m\r\n");
        return;
      }
      connect();
    }

    // Custom fonts (JetBrains Mono, etc.) may still be loading when
    // the pane mounts. Initializing xterm before fonts settle locks
    // in fallback-font cell metrics, so the WebGL atlas and the
    // measured cols/rows drift away from what gets painted — which
    // looks like cursor/prompt overlap in the running shell.
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
    width: 100%;
    height: 100%;
  }
</style>
