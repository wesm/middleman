package workspace

import (
	"context"
	"errors"
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/middleman/internal/workspace/localruntime"
)

func TestRuntimeLifecycleLaunchStopsSessionWhenTmuxRecordFails(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := context.Background()
	createdAt := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	process := &fakeRuntimeProcess{
		launchInfo: localruntime.SessionInfo{
			Key:         "ws-1:helper",
			WorkspaceID: "ws-1",
			TargetKey:   "helper",
			TmuxSession: "middleman-ws-1-helper",
			CreatedAt:   createdAt,
		},
	}
	persistence := &fakeRuntimePersistence{
		recordErr: errors.New("write failed"),
	}
	lifecycle := NewRuntimeLifecycle(process, persistence)

	_, err := lifecycle.LaunchSession(
		ctx,
		RuntimeWorkspace{ID: "ws-1", WorktreePath: "/tmp/ws-1"},
		"helper",
	)

	require.Error(err)
	assert.Contains(err.Error(), "record runtime tmux session")
	assert.Equal([]runtimeStopCall{{
		workspaceID: "ws-1",
		sessionKey:  "ws-1:helper",
	}}, process.stopCalls)
	assert.Equal([]runtimeRecordCall{{
		workspaceID: "ws-1",
		sessionName: "middleman-ws-1-helper",
		targetKey:   "helper",
		createdAt:   createdAt,
	}}, persistence.recordCalls)
}

func TestRuntimeLifecycleLaunchForgetsTmuxRecordWhenSessionAlreadyExited(
	t *testing.T,
) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := context.Background()
	createdAt := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	process := &fakeRuntimeProcess{
		launchInfo: localruntime.SessionInfo{
			Key:         "ws-1:helper",
			WorkspaceID: "ws-1",
			TargetKey:   "helper",
			TmuxSession: "middleman-ws-1-helper",
			CreatedAt:   createdAt,
		},
		listSessions: []localruntime.SessionInfo{{
			Key:         "ws-1:other",
			WorkspaceID: "ws-1",
			TargetKey:   "other",
			TmuxSession: "middleman-ws-1-other",
		}},
	}
	persistence := &fakeRuntimePersistence{}
	lifecycle := NewRuntimeLifecycle(process, persistence)

	session, err := lifecycle.LaunchSession(
		ctx,
		RuntimeWorkspace{ID: "ws-1", WorktreePath: "/tmp/ws-1"},
		"helper",
	)

	require.NoError(err)
	assert.Equal("ws-1:helper", session.Key)
	assert.Equal([]runtimeForgetMissingCall{{
		workspaceID: "ws-1",
		sessionName: "middleman-ws-1-helper",
		createdAt:   createdAt,
	}}, persistence.forgetMissingCalls)
}

func TestRuntimeLifecycleStopSessionForgetsRecordedTmuxSession(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := context.Background()
	process := &fakeRuntimeProcess{
		listSessions: []localruntime.SessionInfo{{
			Key:         "ws-1:helper",
			WorkspaceID: "ws-1",
			TargetKey:   "helper",
			TmuxSession: "middleman-ws-1-helper",
		}},
	}
	persistence := &fakeRuntimePersistence{}
	lifecycle := NewRuntimeLifecycle(process, persistence)

	err := lifecycle.StopSession(ctx, "ws-1", "ws-1:helper")

	require.NoError(err)
	assert.Equal([]runtimeStopCall{{
		workspaceID: "ws-1",
		sessionKey:  "ws-1:helper",
	}}, process.stopCalls)
	assert.Equal([]runtimeForgetCall{{
		workspaceID: "ws-1",
		sessionName: "middleman-ws-1-helper",
	}}, persistence.forgetCalls)
}

func TestRuntimeLifecycleStopSessionFallsBackToStoredTmuxSession(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := context.Background()
	process := &fakeRuntimeProcess{
		stopErr: localruntime.ErrSessionNotFound,
	}
	persistence := &fakeRuntimePersistence{stopStored: true}
	lifecycle := NewRuntimeLifecycle(process, persistence)

	err := lifecycle.StopSession(ctx, "ws-1", "ws-1:helper")

	require.NoError(err)
	assert.Equal([]runtimeStopStoredCall{{
		workspaceID: "ws-1",
		targetKey:   "helper",
	}}, persistence.stopStoredCalls)
	assert.Empty(persistence.forgetCalls)
}

func TestRuntimeLifecycleDeleteWorkspaceHoldsStoppingMarkerUntilDeleteReturns(
	t *testing.T,
) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := context.Background()
	process := &fakeRuntimeProcess{}
	var events []string
	persistence := &fakeRuntimePersistence{
		deleteFunc: func(
			deleteCtx context.Context,
			id string,
			force bool,
			beforeDestructive func(context.Context),
		) ([]string, error) {
			events = append(events, "delete:start")
			assert.True(process.isStopping(id))
			beforeDestructive(deleteCtx)
			assert.True(process.isStopping(id))
			events = append(events, "delete:return")
			return nil, nil
		},
	}
	process.recordEvent = func(event string) {
		events = append(events, event)
	}
	lifecycle := NewRuntimeLifecycle(process, persistence)

	dirty, err := lifecycle.DeleteWorkspace(ctx, "ws-1", true)

	require.NoError(err)
	assert.Empty(dirty)
	assert.Equal([]string{
		"begin:ws-1",
		"delete:start",
		"stop-workspace:ws-1",
		"delete:return",
		"end:ws-1",
	}, events)
	assert.False(process.isStopping("ws-1"))
}

func TestRuntimeLifecycleDeleteWorkspaceDoesNotStopRuntimeOnDirtyRejection(
	t *testing.T,
) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := context.Background()
	process := &fakeRuntimeProcess{}
	var events []string
	persistence := &fakeRuntimePersistence{
		deleteFunc: func(
			deleteCtx context.Context,
			id string,
			force bool,
			beforeDestructive func(context.Context),
		) ([]string, error) {
			events = append(events, "delete:start")
			assert.True(process.isStopping(id))
			return []string{"dirty.txt"}, nil
		},
	}
	process.recordEvent = func(event string) {
		events = append(events, event)
	}
	lifecycle := NewRuntimeLifecycle(process, persistence)

	dirty, err := lifecycle.DeleteWorkspace(ctx, "ws-1", false)

	require.NoError(err)
	assert.Equal([]string{"dirty.txt"}, dirty)
	assert.Equal([]string{
		"begin:ws-1",
		"delete:start",
		"end:ws-1",
	}, events)
	assert.Empty(process.stopWorkspaceCalls)
}

type fakeRuntimeProcess struct {
	launchInfo         localruntime.SessionInfo
	launchErr          error
	ensureShellInfo    localruntime.SessionInfo
	ensureShellErr     error
	listSessions       []localruntime.SessionInfo
	stopErr            error
	stopCalls          []runtimeStopCall
	stopWorkspaceCalls []string
	stopping           map[string]int
	recordEvent        func(string)
}

func (f *fakeRuntimeProcess) EnsureShell(
	ctx context.Context,
	workspaceID string,
	cwd string,
) (localruntime.SessionInfo, error) {
	return f.ensureShellInfo, f.ensureShellErr
}

func (f *fakeRuntimeProcess) Launch(
	ctx context.Context,
	workspaceID string,
	cwd string,
	targetKey string,
) (localruntime.SessionInfo, error) {
	return f.launchInfo, f.launchErr
}

func (f *fakeRuntimeProcess) ListSessions(
	workspaceID string,
) []localruntime.SessionInfo {
	return append([]localruntime.SessionInfo(nil), f.listSessions...)
}

func (f *fakeRuntimeProcess) Stop(
	ctx context.Context,
	workspaceID string,
	sessionKey string,
) error {
	f.stopCalls = append(f.stopCalls, runtimeStopCall{
		workspaceID: workspaceID,
		sessionKey:  sessionKey,
	})
	return f.stopErr
}

func (f *fakeRuntimeProcess) StopWorkspace(
	ctx context.Context,
	workspaceID string,
) {
	f.stopWorkspaceCalls = append(f.stopWorkspaceCalls, workspaceID)
	if f.recordEvent != nil {
		f.recordEvent("stop-workspace:" + workspaceID)
	}
}

func (f *fakeRuntimeProcess) BeginStopping(workspaceID string) {
	if f.stopping == nil {
		f.stopping = map[string]int{}
	}
	f.stopping[workspaceID]++
	if f.recordEvent != nil {
		f.recordEvent("begin:" + workspaceID)
	}
}

func (f *fakeRuntimeProcess) EndStopping(workspaceID string) {
	f.stopping[workspaceID]--
	if f.stopping[workspaceID] <= 0 {
		delete(f.stopping, workspaceID)
	}
	if f.recordEvent != nil {
		f.recordEvent("end:" + workspaceID)
	}
}

func (f *fakeRuntimeProcess) isStopping(workspaceID string) bool {
	return f.stopping[workspaceID] > 0
}

type fakeRuntimePersistence struct {
	recordErr          error
	forgetErr          error
	forgetMissingErr   error
	stopStored         bool
	stopStoredErr      error
	deleteFunc         func(context.Context, string, bool, func(context.Context)) ([]string, error)
	recordCalls        []runtimeRecordCall
	forgetCalls        []runtimeForgetCall
	forgetMissingCalls []runtimeForgetMissingCall
	stopStoredCalls    []runtimeStopStoredCall
}

func (f *fakeRuntimePersistence) RecordRuntimeTmuxSession(
	ctx context.Context,
	workspaceID string,
	sessionName string,
	targetKey string,
	createdAt time.Time,
) error {
	f.recordCalls = append(f.recordCalls, runtimeRecordCall{
		workspaceID: workspaceID,
		sessionName: sessionName,
		targetKey:   targetKey,
		createdAt:   createdAt,
	})
	return f.recordErr
}

func (f *fakeRuntimePersistence) ForgetRuntimeTmuxSession(
	ctx context.Context,
	workspaceID string,
	sessionName string,
) error {
	f.forgetCalls = append(f.forgetCalls, runtimeForgetCall{
		workspaceID: workspaceID,
		sessionName: sessionName,
	})
	return f.forgetErr
}

func (f *fakeRuntimePersistence) ForgetMissingRuntimeTmuxSession(
	ctx context.Context,
	workspaceID string,
	sessionName string,
	createdAt time.Time,
) (bool, error) {
	f.forgetMissingCalls = append(
		f.forgetMissingCalls,
		runtimeForgetMissingCall{
			workspaceID: workspaceID,
			sessionName: sessionName,
			createdAt:   createdAt,
		},
	)
	return true, f.forgetMissingErr
}

func (f *fakeRuntimePersistence) StopStoredRuntimeTmuxSession(
	ctx context.Context,
	workspaceID string,
	targetKey string,
) (bool, error) {
	f.stopStoredCalls = append(f.stopStoredCalls, runtimeStopStoredCall{
		workspaceID: workspaceID,
		targetKey:   targetKey,
	})
	return f.stopStored, f.stopStoredErr
}

func (f *fakeRuntimePersistence) Delete(
	ctx context.Context,
	id string,
	force bool,
	beforeDestructive func(context.Context),
) ([]string, error) {
	if f.deleteFunc == nil {
		return nil, nil
	}
	return f.deleteFunc(ctx, id, force, beforeDestructive)
}

type runtimeStopCall struct {
	workspaceID string
	sessionKey  string
}

type runtimeRecordCall struct {
	workspaceID string
	sessionName string
	targetKey   string
	createdAt   time.Time
}

type runtimeForgetCall struct {
	workspaceID string
	sessionName string
}

type runtimeForgetMissingCall struct {
	workspaceID string
	sessionName string
	createdAt   time.Time
}

type runtimeStopStoredCall struct {
	workspaceID string
	targetKey   string
}
