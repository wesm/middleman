<script lang="ts">
  import { onMount } from "svelte";
  import { Terminal } from "@xterm/xterm";
  import { FitAddon } from "@xterm/addon-fit";
  import { WebglAddon } from "@xterm/addon-webgl";
  import "@xterm/xterm/css/xterm.css";

  const { workspaceId }: { workspaceId: string } = $props();

  const basePath = (window.__BASE_PATH__ ?? "/").replace(/\/$/, "");

  let containerEl: HTMLDivElement;
  let terminal: Terminal | null = $state(null);
  let fitAddon: FitAddon | null = null;
  let ws: WebSocket | null = null;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let reconnectDelay = 1000;
  let resizeObserver: ResizeObserver | null = null;
  let disposed = false;
  let exited = false;
  const encoder = new TextEncoder();

  const MAX_RECONNECT_DELAY = 30000;

  function buildWsUrl(cols: number, rows: number): string {
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const params = `cols=${cols}&rows=${rows}`;
    return (
      `${proto}://${location.host}${basePath}` +
      `/api/v1/workspaces/${encodeURIComponent(workspaceId)}` +
      `/terminal?${params}`
    );
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
          const msg = JSON.parse(ev.data) as { type: string };
          if (msg.type === "exited") {
            exited = true;
            terminal.write(
              "\r\n\x1b[90m[Process exited]\x1b[0m\r\n",
            );
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

  onMount(() => {
    const term = new Terminal({
      theme: {
        background: "#0d1117",
        foreground: "#c9d1d9",
        cursor: "#58a6ff",
      },
      cursorBlink: true,
      fontFamily: "monospace",
      fontSize: 14,
    });
    terminal = term;

    const fit = new FitAddon();
    fitAddon = fit;
    term.loadAddon(fit);

    try {
      term.loadAddon(new WebglAddon());
    } catch {
      // WebGL unavailable; canvas renderer used as fallback.
    }

    term.open(containerEl);
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

    connect();

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
