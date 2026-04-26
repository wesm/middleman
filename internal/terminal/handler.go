package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty/v2"

	"github.com/wesm/middleman/internal/procutil"
	"github.com/wesm/middleman/internal/workspace"
)

// Handler serves WebSocket connections that bridge a
// PTY-attached tmux session to the browser.
type Handler struct {
	Workspaces  *workspace.Manager
	TmuxCommand []string

	mu     sync.Mutex
	active map[string]bool
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

	releaseTerminal, err := h.claimTerminalSlot(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	defer releaseTerminal()

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
		reason := "failed to start tmux"
		if procutil.IsResourceExhausted(tmuxErr) {
			reason = "host process limit reached"
		}
		conn.Close(
			websocket.StatusInternalError,
			reason,
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

	releaseProc, err := procutil.TryAcquire(
		ctx, "terminal attach subprocess capacity",
	)
	if err != nil {
		slog.Error("terminal attach capacity", "err", err)
		conn.Close(
			websocket.StatusInternalError,
			"host process limit reached",
		)
		return
	}
	defer releaseProc()

	winSize := &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	}
	ptmx, err := pty.StartWithSize(cmd, winSize)
	if err != nil {
		slog.Error("pty start", "err", err)
		reason := "failed to start pty"
		if procutil.IsResourceExhausted(err) {
			reason = "host process limit reached"
		}
		conn.Close(
			websocket.StatusInternalError,
			reason,
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
		cmdDone <- processExitCode(cmd.Wait())
	}()

	var exitCode int
	select {
	case exitCode = <-cmdDone:
		// tmux exited normally.
	case <-bridgeDone:
		// Browser disconnected. Cancel context to kill tmux attach.
		cancel()
		_ = ptmx.Close()
		// Wait briefly for cmd to finish.
		select {
		case exitCode = <-cmdDone:
		case <-time.After(2 * time.Second):
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

func (h *Handler) claimTerminalSlot(
	id string,
) (func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active == nil {
		h.active = make(map[string]bool)
	}
	if h.active[id] {
		return nil, fmt.Errorf("workspace terminal already attached")
	}
	h.active[id] = true
	var once sync.Once
	return func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			delete(h.active, id)
		})
	}, nil
}

// Ensure Handler implements http.Handler.
var _ http.Handler = (*Handler)(nil)
