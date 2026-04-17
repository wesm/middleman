package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty/v2"

	"github.com/wesm/middleman/internal/workspace"
)

// Handler serves WebSocket connections that bridge a
// PTY-attached tmux session to the browser.
type Handler struct {
	Workspaces  *workspace.Manager
	TmuxCommand []string
}

type controlMsg struct {
	Type string `json:"type"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

func (h *Handler) ServeHTTP(
	w http.ResponseWriter, r *http.Request,
) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing workspace id", http.StatusBadRequest)
		return
	}

	ws, err := h.Workspaces.Get(r.Context(), id)
	if err != nil {
		slog.Error("get workspace", "id", id, "err", err)
		http.Error(
			w, "internal error",
			http.StatusInternalServerError,
		)
		return
	}
	if ws == nil {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	if ws.Status != "ready" {
		http.Error(
			w,
			fmt.Sprintf("workspace not ready (status: %s)", ws.Status),
			http.StatusConflict,
		)
		return
	}

	cols, rows := parseSize(r)

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		slog.Error("websocket accept", "err", err)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	if tmuxErr := h.Workspaces.EnsureTmux(
		ctx, ws.TmuxSession, ws.WorktreePath,
	); tmuxErr != nil {
		slog.Error("ensure tmux", "err", tmuxErr)
		conn.Close(
			websocket.StatusInternalError,
			"failed to start tmux",
		)
		return
	}

	prefix := h.TmuxCommand
	if len(prefix) == 0 {
		prefix = []string{"tmux"}
	}
	argv := make([]string, 0, len(prefix)+3)
	argv = append(argv, prefix...)
	argv = append(argv, "attach-session", "-t", ws.TmuxSession)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	winSize := &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	}
	ptmx, err := pty.StartWithSize(cmd, winSize)
	if err != nil {
		slog.Error("pty start", "err", err)
		conn.Close(
			websocket.StatusInternalError,
			"failed to start pty",
		)
		return
	}
	defer ptmx.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	// Cancel context when either bridge goroutine exits.
	// This unblocks cmd.Wait below if the browser disconnects.
	bridgeDone := make(chan struct{})

	// PTY -> WebSocket
	go func() {
		defer wg.Done()
		defer func() {
			select {
			case bridgeDone <- struct{}{}:
			default:
			}
		}()
		ptyToWS(ctx, ptmx, conn)
	}()

	// WebSocket -> PTY
	go func() {
		defer wg.Done()
		defer func() {
			select {
			case bridgeDone <- struct{}{}:
			default:
			}
		}()
		wsToPTY(ctx, ptmx, conn)
	}()

	// Wait for tmux exit OR bridge disconnect (whichever first).
	cmdDone := make(chan int, 1)
	go func() {
		exitCode := 0
		if waitErr := cmd.Wait(); waitErr != nil {
			if exit, ok := waitErr.(*exec.ExitError); ok {
				exitCode = exit.ExitCode()
			}
		}
		cmdDone <- exitCode
	}()

	var exitCode int
	select {
	case exitCode = <-cmdDone:
		// tmux exited normally.
	case <-bridgeDone:
		// Browser disconnected. Cancel context to kill tmux attach.
		cancel()
		// Wait briefly for cmd to finish.
		select {
		case exitCode = <-cmdDone:
		case <-ctx.Done():
			exitCode = -1
		}
	}

	exitMsg, _ := json.Marshal(map[string]any{
		"type": "exited",
		"code": exitCode,
	})
	// Use a short-lived context for the final write — the bridge
	// context may already be cancelled from a disconnect path.
	writeCtx, writeCancel := context.WithTimeout(
		context.Background(), 2*time.Second)
	_ = conn.Write(writeCtx, websocket.MessageText, exitMsg)
	writeCancel()
	cancel()
	wg.Wait()
	conn.Close(websocket.StatusNormalClosure, "session ended")
}

func ptyToWS(
	ctx context.Context,
	ptmx *os.File,
	conn *websocket.Conn,
) {
	buf := make([]byte, 32*1024)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			if wErr := conn.Write(
				ctx, websocket.MessageBinary, buf[:n],
			); wErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func wsToPTY(
	ctx context.Context,
	ptmx *os.File,
	conn *websocket.Conn,
) {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		switch typ {
		case websocket.MessageBinary:
			if _, wErr := ptmx.Write(data); wErr != nil {
				return
			}
		case websocket.MessageText:
			var msg controlMsg
			if jsonErr := json.Unmarshal(data, &msg); jsonErr != nil {
				slog.Warn("bad control message", "err", jsonErr)
				continue
			}
			handleControl(ptmx, &msg)
		}
	}
}

func handleControl(ptmx *os.File, msg *controlMsg) {
	if msg.Type != "resize" {
		return
	}
	if msg.Cols <= 0 || msg.Rows <= 0 {
		return
	}
	if err := pty.Setsize(ptmx, &pty.Winsize{
		Rows: uint16(msg.Rows),
		Cols: uint16(msg.Cols),
	}); err != nil {
		slog.Warn("pty resize", "err", err)
	}
}

func parseSize(r *http.Request) (cols, rows int) {
	cols = parseIntParam(r, "cols", 120)
	rows = parseIntParam(r, "rows", 30)
	return cols, rows
}

func parseIntParam(
	r *http.Request, name string, fallback int,
) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

// Ensure Handler implements http.Handler.
var _ http.Handler = (*Handler)(nil)
