package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wesm/middleman/internal/workspace/localruntime"
)

// RuntimeWorkspace is the workspace state the runtime lifecycle needs to
// launch a process without knowing about server response shapes.
type RuntimeWorkspace struct {
	ID           string
	WorktreePath string
}

type runtimeProcessAdapter interface {
	EnsureShell(
		ctx context.Context,
		workspaceID string,
		cwd string,
	) (localruntime.SessionInfo, error)
	Launch(
		ctx context.Context,
		workspaceID string,
		cwd string,
		targetKey string,
	) (localruntime.SessionInfo, error)
	ListSessions(workspaceID string) []localruntime.SessionInfo
	Stop(ctx context.Context, workspaceID string, sessionKey string) error
	StopWorkspace(ctx context.Context, workspaceID string)
	BeginStopping(workspaceID string)
	EndStopping(workspaceID string)
}

type runtimePersistenceAdapter interface {
	RecordRuntimeTmuxSession(
		ctx context.Context,
		workspaceID string,
		sessionName string,
		targetKey string,
		createdAt time.Time,
	) error
	ForgetRuntimeTmuxSession(
		ctx context.Context,
		workspaceID string,
		sessionName string,
	) error
	ForgetMissingRuntimeTmuxSession(
		ctx context.Context,
		workspaceID string,
		sessionName string,
		createdAt time.Time,
	) (bool, error)
	StopStoredRuntimeTmuxSession(
		ctx context.Context,
		workspaceID string,
		targetKey string,
	) (bool, error)
	Delete(
		ctx context.Context,
		id string,
		force bool,
		beforeDestructive func(context.Context),
	) ([]string, error)
}

// RuntimeLifecycle coordinates runtime process state with durable workspace
// ownership state. localruntime.Manager remains the process adapter, while
// Manager remains the persistence/worktree adapter.
type RuntimeLifecycle struct {
	process     runtimeProcessAdapter
	persistence runtimePersistenceAdapter
}

func NewRuntimeLifecycle(
	process runtimeProcessAdapter,
	persistence runtimePersistenceAdapter,
) *RuntimeLifecycle {
	return &RuntimeLifecycle{
		process:     process,
		persistence: persistence,
	}
}

// LaunchSession starts or reuses a runtime session and records any tmux-backed
// ownership row before returning it to callers.
func (l *RuntimeLifecycle) LaunchSession(
	ctx context.Context,
	ws RuntimeWorkspace,
	targetKey string,
) (localruntime.SessionInfo, error) {
	if targetKey == string(localruntime.LaunchTargetPlainShell) {
		return l.process.EnsureShell(ctx, ws.ID, ws.WorktreePath)
	}

	session, err := l.process.Launch(ctx, ws.ID, ws.WorktreePath, targetKey)
	if err != nil {
		return localruntime.SessionInfo{}, err
	}
	if session.TmuxSession == "" {
		return session, nil
	}

	if err := l.persistence.RecordRuntimeTmuxSession(
		ctx, ws.ID, session.TmuxSession, session.TargetKey,
		session.CreatedAt,
	); err != nil {
		_ = l.process.Stop(ctx, ws.ID, session.Key)
		return localruntime.SessionInfo{}, fmt.Errorf(
			"record runtime tmux session: %w", err,
		)
	}
	if runtimeSessionTmuxSession(
		l.process.ListSessions(ws.ID), session.Key,
	) == "" {
		if _, err := l.persistence.ForgetMissingRuntimeTmuxSession(
			ctx, ws.ID, session.TmuxSession, session.CreatedAt,
		); err != nil {
			return localruntime.SessionInfo{}, fmt.Errorf(
				"forget missing runtime tmux session: %w", err,
			)
		}
	}
	return session, nil
}

func (l *RuntimeLifecycle) StopSession(
	ctx context.Context,
	workspaceID string,
	sessionKey string,
) error {
	tmuxSession := runtimeSessionTmuxSession(
		l.process.ListSessions(workspaceID), sessionKey,
	)
	if err := l.process.Stop(ctx, workspaceID, sessionKey); err != nil {
		if errors.Is(err, localruntime.ErrSessionNotFound) {
			if targetKey, ok := runtimeTargetKeyFromSessionKey(
				workspaceID, sessionKey,
			); ok {
				stopped, stopErr := l.persistence.
					StopStoredRuntimeTmuxSession(
						ctx, workspaceID, targetKey,
					)
				if stopErr != nil {
					return fmt.Errorf(
						"stop stored runtime tmux session: %w",
						stopErr,
					)
				}
				if stopped {
					return nil
				}
			}
			return err
		}
		return fmt.Errorf("stop runtime session: %w", err)
	}
	if tmuxSession != "" {
		if err := l.persistence.ForgetRuntimeTmuxSession(
			ctx, workspaceID, tmuxSession,
		); err != nil {
			return fmt.Errorf("forget runtime tmux session: %w", err)
		}
	}
	return nil
}

// DeleteWorkspace keeps the runtime stopping marker active across the whole
// workspace delete flow, while only stopping sessions after the workspace
// adapter's dirty preflight has allowed destructive cleanup to proceed.
func (l *RuntimeLifecycle) DeleteWorkspace(
	ctx context.Context,
	id string,
	force bool,
) ([]string, error) {
	l.process.BeginStopping(id)
	defer l.process.EndStopping(id)
	return l.persistence.Delete(
		ctx, id, force,
		func(stopCtx context.Context) {
			l.process.StopWorkspace(stopCtx, id)
		},
	)
}

func (l *RuntimeLifecycle) ForgetMissingSession(
	ctx context.Context,
	info localruntime.SessionInfo,
) (bool, error) {
	if info.TmuxSession == "" {
		return false, nil
	}
	return l.persistence.ForgetMissingRuntimeTmuxSession(
		ctx, info.WorkspaceID, info.TmuxSession, info.CreatedAt,
	)
}

func runtimeSessionTmuxSession(
	sessions []localruntime.SessionInfo,
	key string,
) string {
	for _, session := range sessions {
		if session.Key == key {
			return session.TmuxSession
		}
	}
	return ""
}

func runtimeTargetKeyFromSessionKey(
	workspaceID string,
	key string,
) (string, bool) {
	targetKey, ok := strings.CutPrefix(key, workspaceID+":")
	return targetKey, ok && targetKey != ""
}
