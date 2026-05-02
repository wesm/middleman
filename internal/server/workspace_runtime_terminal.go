package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/workspace/localruntime"
)

type runtimeTerminalControlMsg struct {
	Type string `json:"type"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

func (s *Server) handleWorkspaceRuntimeSessionTerminal(
	w http.ResponseWriter,
	r *http.Request,
) {
	logWebsocketDebug(
		"runtime terminal websocket request",
		"workspace_id", r.PathValue("id"),
		"session_key", r.PathValue("session_key"),
		"path", r.URL.Path,
		"query", r.URL.RawQuery,
	)
	summary, ok := s.readyRuntimeWorkspaceForHTTP(
		w, r, r.PathValue("id"),
	)
	if !ok {
		return
	}

	attachment, err := s.runtime.AttachSession(
		summary.ID, r.PathValue("session_key"),
	)
	if err != nil {
		logWebsocketDebug(
			"runtime terminal attach failed",
			"workspace_id", summary.ID,
			"session_key", r.PathValue("session_key"),
			"err", err,
		)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.serveRuntimeTerminal(w, r, attachment)
}

func (s *Server) handleWorkspaceRuntimeShellTerminal(
	w http.ResponseWriter,
	r *http.Request,
) {
	logWebsocketDebug(
		"runtime shell websocket request",
		"workspace_id", r.PathValue("id"),
		"path", r.URL.Path,
		"query", r.URL.RawQuery,
	)
	summary, ok := s.readyRuntimeWorkspaceForHTTP(
		w, r, r.PathValue("id"),
	)
	if !ok {
		return
	}

	attachment, err := s.runtime.AttachShell(summary.ID)
	if err != nil {
		logWebsocketDebug(
			"runtime shell attach failed",
			"workspace_id", summary.ID,
			"err", err,
		)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.serveRuntimeTerminal(w, r, attachment)
}

func (s *Server) serveRuntimeTerminal(
	w http.ResponseWriter,
	r *http.Request,
	attachment *localruntime.Attachment,
) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		slog.Error("websocket accept", "err", err)
		attachment.Close()
		return
	}
	info := attachment.Info()
	logWebsocketDebug(
		"runtime terminal websocket accepted",
		"workspace_id", info.WorkspaceID,
		"session_key", info.Key,
		"target_key", info.TargetKey,
	)
	if cols, rows, ok := parseRuntimeTerminalSize(r); ok {
		logWebsocketDebug(
			"runtime terminal initial resize",
			"workspace_id", info.WorkspaceID,
			"session_key", info.Key,
			"cols", cols,
			"rows", rows,
		)
		if err := attachment.Resize(cols, rows); err != nil {
			slog.Warn("runtime terminal initial resize", "err", err)
		}
	}

	exited := bridgeRuntimeAttachment(r.Context(), conn, attachment)
	if exited {
		logWebsocketDebug(
			"runtime terminal websocket closing after session exit",
			"workspace_id", info.WorkspaceID,
			"session_key", info.Key,
		)
		conn.Close(websocket.StatusNormalClosure, "session ended")
	} else {
		logWebsocketDebug(
			"runtime terminal websocket closing after detach",
			"workspace_id", info.WorkspaceID,
			"session_key", info.Key,
		)
		conn.Close(websocket.StatusNormalClosure, "detached")
	}
}

func (s *Server) readyRuntimeWorkspaceForHTTP(
	w http.ResponseWriter,
	r *http.Request,
	id string,
) (*db.WorkspaceSummary, bool) {
	if s.workspaces == nil || s.runtime == nil {
		http.Error(
			w, "workspace runtime not configured",
			http.StatusServiceUnavailable,
		)
		return nil, false
	}

	summary, err := s.workspaces.GetSummary(r.Context(), id)
	if err != nil {
		http.Error(w, "get workspace failed", http.StatusInternalServerError)
		return nil, false
	}
	if summary == nil {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return nil, false
	}
	if summary.Status != "ready" {
		logWebsocketDebug(
			"runtime websocket rejected: workspace not ready",
			"workspace_id", id,
			"status", summary.Status,
		)
		http.Error(
			w,
			"workspace not ready (status: "+summary.Status+")",
			http.StatusConflict,
		)
		return nil, false
	}
	return summary, true
}

func parseRuntimeTerminalSize(
	r *http.Request,
) (cols int, rows int, ok bool) {
	cols, colsOK := parsePositiveQueryInt(r, "cols")
	rows, rowsOK := parsePositiveQueryInt(r, "rows")
	return cols, rows, colsOK && rowsOK
}

func parsePositiveQueryInt(r *http.Request, name string) (int, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, false
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}

func bridgeRuntimeAttachment(
	ctx context.Context,
	conn *websocket.Conn,
	attachment *localruntime.Attachment,
) bool {
	defer attachment.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				logWebsocketDebug("runtime terminal websocket read ended", "err", err)
				return
			}
			switch typ {
			case websocket.MessageBinary:
				if err := attachment.Write(data); err != nil {
					logWebsocketDebug(
						"runtime terminal pty write ended",
						"err", err,
					)
					return
				}
			case websocket.MessageText:
				handleRuntimeTerminalControl(ctx, attachment, data)
			}
		}
	}()

	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		for {
			select {
			case data, ok := <-attachment.Output:
				if !ok {
					return
				}
				if err := conn.Write(
					ctx, websocket.MessageBinary, data,
				); err != nil {
					logWebsocketDebug(
						"runtime terminal websocket write ended",
						"err", err,
					)
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	select {
	case <-attachment.Done:
		select {
		case <-outputDone:
		case <-time.After(100 * time.Millisecond):
		}
		cancel()
		writeRuntimeExit(conn, attachment.Info())
		return true
	case <-inputDone:
		cancel()
		return false
	case <-outputDone:
		select {
		case <-attachment.Done:
			cancel()
			writeRuntimeExit(conn, attachment.Info())
			return true
		case <-time.After(100 * time.Millisecond):
			cancel()
			return false
		}
	case <-ctx.Done():
		return false
	}
}

func handleRuntimeTerminalControl(
	ctx context.Context,
	attachment *localruntime.Attachment,
	data []byte,
) {
	var msg runtimeTerminalControlMsg
	if err := json.Unmarshal(data, &msg); err != nil {
		slog.Warn("bad runtime terminal control message", "err", err)
		return
	}
	info := attachment.Info()
	switch msg.Type {
	case "refresh":
		logWebsocketDebug(
			"runtime terminal refresh requested",
			"workspace_id", info.WorkspaceID,
			"session_key", info.Key,
			"cols", msg.Cols,
			"rows", msg.Rows,
		)
		if msg.Cols > 0 && msg.Rows > 0 {
			if err := attachment.Resize(msg.Cols, msg.Rows); err != nil {
				slog.Warn("runtime terminal refresh resize", "err", err)
			}
		}
		refreshCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := attachment.Refresh(refreshCtx); err != nil {
			slog.Warn("runtime terminal refresh", "err", err)
		}
		return
	case "resize":
		logWebsocketDebug(
			"runtime terminal resize requested",
			"workspace_id", info.WorkspaceID,
			"session_key", info.Key,
			"cols", msg.Cols,
			"rows", msg.Rows,
		)
		if err := attachment.Resize(msg.Cols, msg.Rows); err != nil {
			slog.Warn("runtime terminal resize", "err", err)
		}
	}
}

func writeRuntimeExit(
	conn *websocket.Conn,
	info localruntime.SessionInfo,
) {
	exitCode := -1
	if info.ExitCode != nil {
		exitCode = *info.ExitCode
	}
	exitMsg, _ := json.Marshal(map[string]any{
		"type": "exited",
		"code": exitCode,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = conn.Write(ctx, websocket.MessageText, exitMsg)
}
