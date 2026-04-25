package server

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/wesm/middleman/internal/terminal"
)

func (s *Server) registerTerminalAPI(api huma.API) {
	handler := &terminal.Handler{
		Workspaces:  s.workspaces,
		TmuxCommand: s.cfg.TmuxCommand(),
	}
	op := &huma.Operation{
		OperationID: "connect-workspace-terminal",
		Method:      http.MethodGet,
		Path:        "/workspaces/{id}/terminal",
		Hidden:      true,
	}
	api.Adapter().Handle(op, func(ctx huma.Context) {
		r, w := humago.Unwrap(ctx)
		handler.ServeHTTP(w, r)
	})

	if s.runtime == nil {
		return
	}
	sessionOp := &huma.Operation{
		OperationID: "connect-workspace-runtime-session-terminal",
		Method:      http.MethodGet,
		Path:        "/workspaces/{id}/runtime/sessions/{session_key}/terminal",
		Hidden:      true,
	}
	api.Adapter().Handle(sessionOp, func(ctx huma.Context) {
		r, w := humago.Unwrap(ctx)
		s.handleWorkspaceRuntimeSessionTerminal(w, r)
	})

	shellOp := &huma.Operation{
		OperationID: "connect-workspace-runtime-shell-terminal",
		Method:      http.MethodGet,
		Path:        "/workspaces/{id}/runtime/shell/terminal",
		Hidden:      true,
	}
	api.Adapter().Handle(shellOp, func(ctx huma.Context) {
		r, w := humago.Unwrap(ctx)
		s.handleWorkspaceRuntimeShellTerminal(w, r)
	})
}
