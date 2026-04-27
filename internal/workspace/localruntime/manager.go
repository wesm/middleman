package localruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty/v2"
)

type SessionStatus string

const (
	SessionStatusStarting SessionStatus = "starting"
	SessionStatusRunning  SessionStatus = "running"
	SessionStatusExited   SessionStatus = "exited"
	SessionStatusError    SessionStatus = "error"
)

var (
	errManagerShutdown   = errors.New("runtime manager is shut down")
	ErrSessionNotFound   = errors.New("runtime session not found")
	errWorkspaceStopping = errors.New(
		"workspace is being stopped",
	)
)

type SessionInfo struct {
	Key         string           `json:"key"`
	WorkspaceID string           `json:"workspace_id"`
	TargetKey   string           `json:"target_key"`
	Label       string           `json:"label"`
	Kind        LaunchTargetKind `json:"kind"`
	Status      SessionStatus    `json:"status"`
	CreatedAt   time.Time        `json:"created_at"`
	ExitedAt    *time.Time       `json:"exited_at,omitempty"`
	ExitCode    *int             `json:"exit_code,omitempty"`
	TmuxSession string           `json:"-"`
}

type Options struct {
	Targets      []LaunchTarget
	ShellCommand []string
	TmuxCommand  []string
	// TmuxOwnerMarker tags tmux-backed agent sessions so workspace startup
	// cleanup can identify middleman-owned runtime sessions that were created
	// before their durable DB row was written.
	TmuxOwnerMarker string
	// WrapAgentSessionsInTmux starts agent targets under tmux when
	// the tmux launch target is available. When tmux is unavailable
	// or this is false, agents run directly in the runtime PTY.
	WrapAgentSessionsInTmux bool
	// StripEnvVars names additional env vars to strip beyond the
	// built-in credential prefixes (e.g. a configured token env).
	StripEnvVars []string
}

type Manager struct {
	mu               sync.Mutex
	targets          map[string]LaunchTarget
	targetsList      []LaunchTarget
	sessions         map[string]*session
	shells           map[string]*session
	shellCommand     []string
	tmuxCommand      []string
	tmuxOwnerMarker  string
	wrapAgentsInTmux bool
	stripEnvVars     []string
	startLocks       map[string]*sync.Mutex
	stoppingWS       map[string]int
	inflightWS       map[string]int
	inflightCh       map[string]chan struct{}
	startWG          sync.WaitGroup
	closed           bool
}

// maxSessionOutputReplay caps how many bytes of recent PTY output
// the session retains for replay when a new subscriber attaches.
// Sized to comfortably hold an agent boot banner plus the first
// prompt — without it, a fast subscribe-after-launch flow can miss
// startup output entirely.
const maxSessionOutputReplay = 64 * 1024

var (
	alternateScreenEnterSequences = [][]byte{
		[]byte("\x1b[?47h"),
		[]byte("\x1b[?1047h"),
		[]byte("\x1b[?1049h"),
	}
	alternateScreenExitSequences = [][]byte{
		[]byte("\x1b[?47l"),
		[]byte("\x1b[?1047l"),
		[]byte("\x1b[?1049l"),
	}
	maxAlternateScreenSequenceLen = maxByteSliceLen(
		append(
			slices.Clone(alternateScreenEnterSequences),
			alternateScreenExitSequences...,
		),
	)
)

type session struct {
	mu                    sync.Mutex
	info                  SessionInfo
	cmd                   *exec.Cmd
	ptmx                  *os.File
	tmuxSession           string
	done                  chan struct{}
	subscribers           map[chan []byte]struct{}
	outputBuffer          []byte
	outputClosed          bool
	alternateScreenActive bool
	alternateScreenTail   []byte
	stopOnce              sync.Once
}

type Attachment struct {
	Output <-chan []byte
	Done   <-chan struct{}

	info   func() SessionInfo
	write  func([]byte) error
	resize func(cols, rows int) error
	close  func()
}

func NewManager(options Options) *Manager {
	targets := make(map[string]LaunchTarget, len(options.Targets))
	targetsList := make([]LaunchTarget, 0, len(options.Targets))
	for _, target := range options.Targets {
		cloned := cloneTarget(target)
		targets[target.Key] = cloned
		targetsList = append(targetsList, cloneTarget(cloned))
	}
	return &Manager{
		targets:          targets,
		targetsList:      targetsList,
		sessions:         make(map[string]*session),
		shells:           make(map[string]*session),
		shellCommand:     append([]string(nil), options.ShellCommand...),
		tmuxCommand:      append([]string(nil), options.TmuxCommand...),
		tmuxOwnerMarker:  options.TmuxOwnerMarker,
		wrapAgentsInTmux: options.WrapAgentSessionsInTmux,
		stripEnvVars:     dedupeStrings(options.StripEnvVars),
		startLocks:       make(map[string]*sync.Mutex),
		stoppingWS:       make(map[string]int),
		inflightWS:       make(map[string]int),
		inflightCh:       make(map[string]chan struct{}),
	}
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func maxByteSliceLen(slices [][]byte) int {
	maxLen := 0
	for _, slice := range slices {
		if len(slice) > maxLen {
			maxLen = len(slice)
		}
	}
	return maxLen
}

func (m *Manager) Launch(
	ctx context.Context,
	workspaceID string,
	cwd string,
	targetKey string,
) (SessionInfo, error) {
	slog.Debug(
		"runtime launch requested",
		"workspace_id", workspaceID,
		"target_key", targetKey,
		"cwd", cwd,
	)
	if err := ctx.Err(); err != nil {
		return SessionInfo{}, err
	}

	target, err := m.target(targetKey)
	if err != nil {
		return SessionInfo{}, err
	}
	if !target.Available {
		reason := target.DisabledReason
		if reason == "" {
			reason = "target not available"
		}
		return SessionInfo{}, fmt.Errorf(
			"target %q not available: %s", targetKey, reason,
		)
	}
	if len(target.Command) == 0 || target.Command[0] == "" {
		return SessionInfo{}, fmt.Errorf(
			"target %q has no command", targetKey,
		)
	}

	key := sessionKey(workspaceID, targetKey)
	startMu := m.startLock(key)
	startMu.Lock()
	defer startMu.Unlock()

	if err := m.ensureOpen(); err != nil {
		return SessionInfo{}, err
	}
	if existing := m.runningSession(m.sessions, key); existing != nil {
		slog.Debug(
			"runtime launch reused existing session",
			"workspace_id", workspaceID,
			"session_key", key,
			"target_key", targetKey,
		)
		return existing.snapshot(), nil
	}

	if err := m.beginStart(); err != nil {
		return SessionInfo{}, err
	}
	defer m.finishStart()

	if err := m.claimInflight(workspaceID); err != nil {
		return SessionInfo{}, err
	}
	defer m.releaseInflight(workspaceID)

	launch, err := m.launchCommand(target, workspaceID, cwd)
	if err != nil {
		slog.Debug(
			"runtime launch command failed",
			"workspace_id", workspaceID,
			"session_key", key,
			"target_key", targetKey,
			"err", err,
		)
		return SessionInfo{}, err
	}
	slog.Debug(
		"runtime launch starting session",
		"workspace_id", workspaceID,
		"session_key", key,
		"target_key", targetKey,
		"kind", target.Kind,
		"tmux_session", launch.TmuxSession,
	)

	started, err := startSession(SessionInfo{
		Key:         key,
		WorkspaceID: workspaceID,
		TargetKey:   targetKey,
		Label:       target.Label,
		Kind:        target.Kind,
		Status:      SessionStatusStarting,
		CreatedAt:   time.Now().UTC(),
	}, launch.Command, cwd, m.stripEnvVars)
	if err != nil {
		slog.Debug(
			"runtime launch start failed",
			"workspace_id", workspaceID,
			"session_key", key,
			"target_key", targetKey,
			"err", err,
		)
		return SessionInfo{}, err
	}
	started.tmuxSession = launch.TmuxSession
	go started.watch()

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = m.stopSession(ctx, started)
		waitSessionDone(started)
		slog.Debug(
			"runtime launch discarded session after shutdown",
			"workspace_id", workspaceID,
			"session_key", key,
		)
		return SessionInfo{}, errManagerShutdown
	}
	m.sessions[key] = started
	m.mu.Unlock()
	slog.Debug(
		"runtime launch session stored",
		"workspace_id", workspaceID,
		"session_key", key,
		"target_key", targetKey,
	)

	return started.snapshot(), nil
}

func (m *Manager) LaunchTargets() []LaunchTarget {
	m.mu.Lock()
	defer m.mu.Unlock()

	targets := make([]LaunchTarget, 0, len(m.targetsList))
	for _, target := range m.targetsList {
		targets = append(targets, cloneTarget(target))
	}
	return targets
}

func (m *Manager) ListSessions(workspaceID string) []SessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	sessions := make([]SessionInfo, 0)
	for _, s := range m.sessions {
		info := s.snapshot()
		if info.WorkspaceID == workspaceID {
			sessions = append(sessions, info)
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.Before(sessions[j].CreatedAt)
	})
	return sessions
}

// TmuxSessions returns runtime-owned tmux sessions associated with
// a workspace. These sessions are additional activity sources for
// the workspace sidebar; the persisted workspace tmux session remains
// owned by internal/workspace.Manager.
func (m *Manager) TmuxSessions(workspaceID string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	sessions := make([]string, 0)
	for _, s := range m.sessions {
		info := s.snapshot()
		if info.WorkspaceID == workspaceID && s.tmuxSession != "" {
			sessions = append(sessions, s.tmuxSession)
		}
	}
	sort.Strings(sessions)
	return sessions
}

func (m *Manager) Stop(
	ctx context.Context,
	workspaceID string,
	sessionKey string,
) error {
	s, ok := m.session(workspaceID, sessionKey)
	if !ok {
		return fmt.Errorf("%w: %q", ErrSessionNotFound, sessionKey)
	}

	cleanupErr := m.stopSession(ctx, s)
	if cleanupErr != nil {
		return cleanupErr
	}
	m.removeIfSame(workspaceID, sessionKey, s)
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// StopWorkspace stops every running agent session and shell that
// belongs to workspaceID. It is intended to be called when a
// workspace is deleted so launched processes do not survive the
// worktree they were started in.
func (m *Manager) StopWorkspace(
	ctx context.Context,
	workspaceID string,
) {
	// 1. Mark the workspace as stopping under the manager mutex.
	//    New Launch/EnsureShell calls that observe this marker bail
	//    out via claimInflight before spawning a process.
	m.mu.Lock()
	m.stoppingWS[workspaceID]++
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.stoppingWS[workspaceID]--
		if m.stoppingWS[workspaceID] <= 0 {
			delete(m.stoppingWS, workspaceID)
		}
		m.mu.Unlock()
	}()

	// 2. Drain any Launch/EnsureShell calls that passed claimInflight
	//    before the marker was set. They are mid-startSession; once
	//    they finish, their processes are in m.sessions/m.shells and
	//    will be picked up by the snapshot below. Without this drain
	//    a launch in flight at step 1 can insert after the snapshot
	//    and leave a process alive for the deleted worktree.
	if err := m.waitInflight(ctx, workspaceID); err != nil {
		return
	}

	// 3. Snapshot and remove all sessions/shells for the workspace.
	m.mu.Lock()
	stopping := make([]*session, 0)
	for key, s := range m.sessions {
		if s.snapshot().WorkspaceID == workspaceID {
			delete(m.sessions, key)
			stopping = append(stopping, s)
		}
	}
	for key, s := range m.shells {
		if s.snapshot().WorkspaceID == workspaceID {
			delete(m.shells, key)
			stopping = append(stopping, s)
		}
	}
	m.mu.Unlock()

	for _, s := range stopping {
		if err := m.stopSession(ctx, s); err != nil {
			slog.Warn(
				"stop workspace runtime session",
				"workspace_id", workspaceID,
				"session_key", s.snapshot().Key,
				"err", err,
			)
		}
	}
	for _, s := range stopping {
		select {
		case <-s.done:
		case <-ctx.Done():
			return
		}
	}
}

func (m *Manager) stopSession(ctx context.Context, s *session) error {
	if s == nil {
		return nil
	}
	var cleanupErr error
	if s.tmuxSession != "" {
		if err := m.killTmuxSession(ctx, s.tmuxSession); err != nil {
			cleanupErr = fmt.Errorf(
				"kill tmux session %q: %w", s.tmuxSession, err,
			)
		}
	}
	s.stop()
	return cleanupErr
}

func (m *Manager) killTmuxSession(
	ctx context.Context,
	session string,
) error {
	if session == "" {
		return nil
	}
	command := append([]string(nil), m.tmuxCommand...)
	if len(command) == 0 {
		command = []string{"tmux"}
	}
	if len(command) == 0 || command[0] == "" {
		return nil
	}
	args := append(command[1:], "kill-session", "-t", session)
	cmd := exec.CommandContext(ctx, command[0], args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil || isTmuxSessionAbsent(stderr.Bytes(), err) {
		return nil
	}
	msg := strings.TrimSpace(stderr.String())
	if msg == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, msg)
}

func isTmuxSessionAbsent(stderr []byte, err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		return false
	}
	msg := string(stderr)
	return strings.Contains(msg, "can't find session") ||
		strings.Contains(msg, "no server running") ||
		(strings.Contains(msg, "error connecting to") &&
			strings.Contains(msg, "No such file or directory"))
}

// BeginStopping holds the stopping marker for workspaceID without
// running StopWorkspace. Use it to extend the marker's lifetime
// across higher-level operations — for example, the workspace
// deletion handler holds it from the start of Delete through the
// destructive cleanup and DB removal so a concurrent Launch cannot
// spawn a process into a worktree that is about to disappear. Must
// be paired with EndStopping.
func (m *Manager) BeginStopping(workspaceID string) {
	m.mu.Lock()
	m.stoppingWS[workspaceID]++
	m.mu.Unlock()
}

// EndStopping releases a marker held by BeginStopping. Decrementing
// to zero unblocks new launches for the workspace.
func (m *Manager) EndStopping(workspaceID string) {
	m.mu.Lock()
	m.stoppingWS[workspaceID]--
	if m.stoppingWS[workspaceID] <= 0 {
		delete(m.stoppingWS, workspaceID)
	}
	m.mu.Unlock()
}

// claimInflight registers a starting Launch/EnsureShell so a
// concurrent StopWorkspace can wait for it to finish inserting
// before snapshotting. Rejects if the workspace is already being
// stopped.
func (m *Manager) claimInflight(workspaceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stoppingWS[workspaceID] > 0 {
		return errWorkspaceStopping
	}
	m.inflightWS[workspaceID]++
	return nil
}

func (m *Manager) releaseInflight(workspaceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inflightWS[workspaceID]--
	if m.inflightWS[workspaceID] <= 0 {
		delete(m.inflightWS, workspaceID)
		if ch, ok := m.inflightCh[workspaceID]; ok {
			close(ch)
			delete(m.inflightCh, workspaceID)
		}
	}
}

// waitInflight blocks until every claimInflight for workspaceID
// that completed before this call has been released. New claims
// are rejected by the stoppingWS marker the caller already holds.
func (m *Manager) waitInflight(
	ctx context.Context,
	workspaceID string,
) error {
	m.mu.Lock()
	if m.inflightWS[workspaceID] == 0 {
		m.mu.Unlock()
		return nil
	}
	ch, ok := m.inflightCh[workspaceID]
	if !ok {
		ch = make(chan struct{})
		m.inflightCh[workspaceID] = ch
	}
	m.mu.Unlock()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) AttachSession(
	workspaceID string,
	key string,
) (*Attachment, error) {
	slog.Debug(
		"runtime terminal attach requested",
		"workspace_id", workspaceID,
		"session_key", key,
	)
	m.mu.Lock()
	s := m.sessions[key]
	m.mu.Unlock()
	attachment, err := attachToSession(s, workspaceID, key)
	if err != nil {
		slog.Debug(
			"runtime terminal attach rejected",
			"workspace_id", workspaceID,
			"session_key", key,
			"err", err,
		)
		return nil, err
	}
	slog.Debug(
		"runtime terminal attach accepted",
		"workspace_id", workspaceID,
		"session_key", key,
	)
	return attachment, nil
}

func (m *Manager) AttachShell(
	workspaceID string,
) (*Attachment, error) {
	key := sessionKey(workspaceID, "shell")
	slog.Debug(
		"runtime shell attach requested",
		"workspace_id", workspaceID,
		"session_key", key,
	)
	m.mu.Lock()
	s := m.shells[key]
	m.mu.Unlock()
	attachment, err := attachToSession(s, workspaceID, key)
	if err != nil {
		slog.Debug(
			"runtime shell attach rejected",
			"workspace_id", workspaceID,
			"session_key", key,
			"err", err,
		)
		return nil, err
	}
	slog.Debug(
		"runtime shell attach accepted",
		"workspace_id", workspaceID,
		"session_key", key,
	)
	return attachment, nil
}

func (m *Manager) EnsureShell(
	ctx context.Context,
	workspaceID string,
	cwd string,
) (SessionInfo, error) {
	slog.Debug(
		"runtime shell ensure requested",
		"workspace_id", workspaceID,
		"cwd", cwd,
	)
	if err := ctx.Err(); err != nil {
		return SessionInfo{}, err
	}

	key := sessionKey(workspaceID, "shell")
	startMu := m.startLock(key)
	startMu.Lock()
	defer startMu.Unlock()

	if err := m.ensureOpen(); err != nil {
		return SessionInfo{}, err
	}
	if existing := m.runningSession(m.shells, key); existing != nil {
		slog.Debug(
			"runtime shell ensure reused existing session",
			"workspace_id", workspaceID,
			"session_key", key,
		)
		return existing.snapshot(), nil
	}

	command := append([]string(nil), m.shellCommand...)
	if len(command) == 0 {
		command = defaultShellCommand()
	}
	if err := m.beginStart(); err != nil {
		return SessionInfo{}, err
	}
	defer m.finishStart()

	if err := m.claimInflight(workspaceID); err != nil {
		return SessionInfo{}, err
	}
	defer m.releaseInflight(workspaceID)

	started, err := startSession(SessionInfo{
		Key:         key,
		WorkspaceID: workspaceID,
		TargetKey:   "plain_shell",
		Label:       "Shell",
		Kind:        LaunchTargetPlainShell,
		Status:      SessionStatusStarting,
		CreatedAt:   time.Now().UTC(),
	}, command, cwd, m.stripEnvVars)
	if err != nil {
		slog.Debug(
			"runtime shell start failed",
			"workspace_id", workspaceID,
			"session_key", key,
			"err", err,
		)
		return SessionInfo{}, err
	}
	go started.watch()

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = m.stopSession(ctx, started)
		waitSessionDone(started)
		slog.Debug(
			"runtime shell discarded session after shutdown",
			"workspace_id", workspaceID,
			"session_key", key,
		)
		return SessionInfo{}, errManagerShutdown
	}
	m.shells[key] = started
	m.mu.Unlock()
	slog.Debug(
		"runtime shell session stored",
		"workspace_id", workspaceID,
		"session_key", key,
	)

	return started.snapshot(), nil
}

func (a *Attachment) Write(data []byte) error {
	if a == nil || a.write == nil {
		return errors.New("attachment is closed")
	}
	return a.write(data)
}

func (a *Attachment) Resize(cols, rows int) error {
	if a == nil || a.resize == nil {
		return errors.New("attachment is closed")
	}
	return a.resize(cols, rows)
}

func (a *Attachment) Info() SessionInfo {
	if a == nil || a.info == nil {
		return SessionInfo{}
	}
	return a.info()
}

func (a *Attachment) Close() {
	if a != nil && a.close != nil {
		a.close()
	}
}

func (m *Manager) ShellSession(workspaceID string) *SessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, s := range m.shells {
		info := s.snapshot()
		if info.WorkspaceID == workspaceID {
			return &info
		}
	}
	return nil
}

func (m *Manager) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()

	m.startWG.Wait()

	m.mu.Lock()
	sessions := make([]*session, 0, len(m.sessions)+len(m.shells))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	for _, s := range m.shells {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*session)
	m.shells = make(map[string]*session)
	m.mu.Unlock()

	waiting := make([]*session, 0, len(sessions))
	for _, s := range sessions {
		if s.tmuxSession != "" {
			s.detach()
			continue
		}
		s.stop()
		waiting = append(waiting, s)
	}
	for _, s := range waiting {
		select {
		case <-s.done:
		case <-ctx.Done():
			return
		}
	}
}

func (m *Manager) ensureOpen() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errManagerShutdown
	}
	return nil
}

func (m *Manager) startLock(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()

	startMu := m.startLocks[key]
	if startMu == nil {
		startMu = &sync.Mutex{}
		m.startLocks[key] = startMu
	}
	return startMu
}

func (m *Manager) beginStart() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errManagerShutdown
	}
	m.startWG.Add(1)
	return nil
}

func (m *Manager) finishStart() {
	m.startWG.Done()
}

func (m *Manager) target(key string) (LaunchTarget, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	target, ok := m.targets[key]
	if !ok {
		return LaunchTarget{}, fmt.Errorf("target not found: %s", key)
	}
	return cloneTarget(target), nil
}

type launchCommand struct {
	Command     []string
	TmuxSession string
}

func (m *Manager) launchCommand(
	target LaunchTarget,
	workspaceID string,
	cwd string,
) (launchCommand, error) {
	command := append([]string(nil), target.Command...)
	if target.Kind != LaunchTargetAgent || !m.wrapAgentsInTmux {
		return launchCommand{Command: command}, nil
	}

	tmux, err := m.target(string(LaunchTargetTmux))
	if err != nil || !tmux.Available {
		return launchCommand{Command: command}, nil
	}
	tmuxCommand := append([]string(nil), m.tmuxCommand...)
	if len(tmuxCommand) == 0 {
		tmuxCommand = append([]string(nil), tmux.Command...)
	}
	if len(tmuxCommand) == 0 {
		tmuxCommand = []string{"tmux"}
	}
	resolvedAgentCommand := append([]string(nil), command...)
	resolvedPath, err := resolveExecutable(resolvedAgentCommand[0])
	if err != nil {
		return launchCommand{}, err
	}
	resolvedAgentCommand[0] = resolvedPath

	tmuxSession := tmuxSessionName(workspaceID, target.Key)

	agentEnv := append(
		sessionEnvironment(tmuxAgentEnvironment(os.Environ()), m.stripEnvVars),
		"TERM=xterm-256color",
	)
	agentCommand := "exec " + shellEnvCommand(agentEnv, resolvedAgentCommand)
	if m.tmuxOwnerMarker != "" {
		return launchCommand{
			Command: m.launchTmuxOwnedCommand(
				tmuxCommand, tmuxSession, cwd, agentCommand,
			),
			TmuxSession: tmuxSession,
		}, nil
	}

	wrapped := append(
		tmuxCommand,
		"new-session",
		"-A",
		"-s",
		tmuxSession,
	)
	if cwd != "" {
		wrapped = append(wrapped, "-c", cwd)
	}
	wrapped = append(wrapped, agentCommand)
	return launchCommand{Command: wrapped, TmuxSession: tmuxSession}, nil
}

func (m *Manager) launchTmuxOwnedCommand(
	tmuxCommand []string,
	tmuxSession string,
	cwd string,
	agentCommand string,
) []string {
	hasSession := shellCommand(append(
		append([]string(nil), tmuxCommand...),
		"has-session", "-t", tmuxSession,
	))
	newSessionArgs := append(
		append([]string(nil), tmuxCommand...),
		"new-session", "-d", "-s", tmuxSession,
	)
	if cwd != "" {
		newSessionArgs = append(newSessionArgs, "-c", cwd)
	}
	newSessionArgs = append(
		newSessionArgs,
		agentCommand,
		";",
		"set-option", "-q", "-t", tmuxSession,
		"@middleman_owner", m.tmuxOwnerMarker,
	)
	newSession := shellCommand(newSessionArgs)
	setOwner := shellCommand(append(
		append([]string(nil), tmuxCommand...),
		"set-option", "-q", "-t", tmuxSession,
		"@middleman_owner", m.tmuxOwnerMarker,
	))
	killSession := shellCommand(append(
		append([]string(nil), tmuxCommand...),
		"kill-session", "-t", tmuxSession,
	))
	attachSession := shellCommand(append(
		append([]string(nil), tmuxCommand...),
		"attach-session", "-t", tmuxSession,
	))
	script := fmt.Sprintf(
		"created=0\n"+
			"if ! %s >/dev/null 2>&1; then\n"+
			"  created=1\n"+
			"  if ! %s; then\n"+
			"    %s >/dev/null 2>&1 || true\n"+
			"    exit 1\n"+
			"  fi\n"+
			"fi\n"+
			"if ! %s; then\n"+
			"  if [ \"$created\" = \"1\" ]; then\n"+
			"    %s >/dev/null 2>&1 || true\n"+
			"  fi\n"+
			"  exit 1\n"+
			"fi\n"+
			"exec %s",
		hasSession, newSession, killSession, setOwner, killSession,
		attachSession,
	)
	return []string{"/bin/sh", "-lc", script}
}

func tmuxSessionName(workspaceID string, targetKey string) string {
	sum := sha256.Sum256([]byte(targetKey))
	return "middleman-" + tmuxSessionSafeComponent(workspaceID) + "-" +
		hex.EncodeToString(sum[:8])
}

func tmuxSessionSafeComponent(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_' || r == '-':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func shellCommand(command []string) string {
	parts := make([]string, 0, len(command)+1)
	for _, arg := range command {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func shellEnvCommand(env []string, command []string) string {
	args := make([]string, 0, len(env)+len(command))
	for _, kv := range env {
		if strings.Contains(kv, "=") {
			args = append(args, kv)
		}
	}
	args = append(args, command...)
	return "env -i " + shellCommand(args)
}

func tmuxAgentEnvironment(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		key := kv[:eq]
		if isTmuxAgentEnvironmentKey(key) {
			out = append(out, kv)
		}
	}
	return out
}

func isTmuxAgentEnvironmentKey(key string) bool {
	switch key {
	case "HOME", "PATH", "SHELL", "USER", "LOGNAME", "LANG",
		"LC_ALL", "LC_CTYPE", "TMPDIR", "SSH_AUTH_SOCK",
		"XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME":
		return true
	default:
		return strings.HasPrefix(key, "LC_")
	}
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (m *Manager) runningSession(
	sessions map[string]*session,
	key string,
) *session {
	m.mu.Lock()
	defer m.mu.Unlock()

	s := sessions[key]
	if s == nil {
		return nil
	}
	info := s.snapshot()
	if info.Status == SessionStatusRunning ||
		info.Status == SessionStatusStarting {
		return s
	}
	delete(sessions, key)
	return nil
}

func (m *Manager) session(
	workspaceID string,
	key string,
) (*session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[key]; ok &&
		s.snapshot().WorkspaceID == workspaceID {
		return s, true
	}
	if s, ok := m.shells[key]; ok &&
		s.snapshot().WorkspaceID == workspaceID {
		return s, true
	}
	return nil, false
}

func (m *Manager) removeIfSame(
	workspaceID string,
	key string,
	s *session,
) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if current, ok := m.sessions[key]; ok &&
		current == s &&
		current.snapshot().WorkspaceID == workspaceID {
		delete(m.sessions, key)
		return
	}
	if current, ok := m.shells[key]; ok &&
		current == s &&
		current.snapshot().WorkspaceID == workspaceID {
		delete(m.shells, key)
	}
}

func startSession(
	info SessionInfo,
	command []string,
	cwd string,
	extraStripVars []string,
) (*session, error) {
	if len(command) == 0 || command[0] == "" {
		return nil, errors.New("session command is empty")
	}

	// Resolve the executable to an absolute path BEFORE setting
	// cmd.Dir to the workspace worktree. exec.Command treats names
	// without separators as PATH lookups but treats names like
	// "./agent" or "scripts/codex" as paths relative to cmd.Dir,
	// which would let a malicious PR drop an executable into the
	// worktree and gain code execution when the maintainer launches
	// the agent. Reject relative paths and require all other names
	// to resolve via PATH.
	resolvedPath, err := resolveExecutable(command[0])
	if err != nil {
		return nil, err
	}
	slog.Debug(
		"runtime session resolving command",
		"workspace_id", info.WorkspaceID,
		"session_key", info.Key,
		"target_key", info.TargetKey,
		"program", resolvedPath,
		"argc", len(command),
		"cwd", cwd,
	)

	cmd := exec.Command(resolvedPath, command[1:]...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	// Pass through a sanitized environment so launched shells and
	// agents do not inherit middleman's GitHub credentials. See
	// sessionEnvironment for the allow/deny rules.
	cmd.Env = append(
		sessionEnvironment(os.Environ(), extraStripVars),
		"TERM=xterm-256color",
	)

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: 30,
		Cols: 120,
	})
	if err != nil {
		return nil, fmt.Errorf("start pty: %w", err)
	}
	slog.Debug(
		"runtime session pty started",
		"workspace_id", info.WorkspaceID,
		"session_key", info.Key,
		"target_key", info.TargetKey,
		"pid", cmd.Process.Pid,
	)

	info.Status = SessionStatusRunning
	s := &session{
		info:        info,
		cmd:         cmd,
		ptmx:        ptmx,
		done:        make(chan struct{}),
		subscribers: make(map[chan []byte]struct{}),
	}
	go s.drainOutput()
	return s, nil
}

func (s *session) snapshot() SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	info := s.info
	info.TmuxSession = s.tmuxSession
	if s.info.ExitedAt != nil {
		exitedAt := *s.info.ExitedAt
		info.ExitedAt = &exitedAt
	}
	if s.info.ExitCode != nil {
		exitCode := *s.info.ExitCode
		info.ExitCode = &exitCode
	}
	return info
}

func (s *session) watch() {
	exitCode := waitExitCode(s.cmd.Wait())
	now := time.Now().UTC()

	s.mu.Lock()
	s.info.Status = SessionStatusExited
	s.info.ExitedAt = &now
	s.info.ExitCode = &exitCode
	info := s.info
	s.mu.Unlock()

	_ = s.ptmx.Close()
	close(s.done)
	slog.Debug(
		"runtime session exited",
		"workspace_id", info.WorkspaceID,
		"session_key", info.Key,
		"target_key", info.TargetKey,
		"exit_code", exitCode,
	)
}

func (s *session) drainOutput() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			s.broadcast(buf[:n])
		}
		if err != nil {
			s.closeSubscribers()
			return
		}
	}
}

func (s *session) broadcast(data []byte) {
	chunk := append([]byte(nil), data...)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.appendReplayOutputLocked(chunk)

	for ch := range s.subscribers {
		select {
		case ch <- chunk:
		default:
			delete(s.subscribers, ch)
			close(ch)
		}
	}
}

type alternateScreenEvent struct {
	start  int
	end    int
	active bool
}

func (s *session) appendReplayOutputLocked(chunk []byte) {
	// Alternate-screen TUIs are stateful. Replaying a suffix of their
	// screen history into a fresh terminal can corrupt the attach, so
	// keep only normal-screen output for future subscribers.
	scan := append(slices.Clone(s.alternateScreenTail), chunk...)
	events := alternateScreenEvents(scan)
	tailLen := len(s.alternateScreenTail)
	active := s.alternateScreenActive
	normalStart := 0

	for _, event := range events {
		if event.end <= tailLen {
			continue
		}
		chunkStart := max(event.start-tailLen, 0)
		chunkEnd := min(event.end-tailLen, len(chunk))
		if !active && chunkStart > normalStart {
			s.appendOutputBufferLocked(chunk[normalStart:chunkStart])
		}
		if event.active {
			s.outputBuffer = nil
		}
		active = event.active
		normalStart = chunkEnd
	}

	if !active && normalStart < len(chunk) {
		s.appendOutputBufferLocked(chunk[normalStart:])
	}
	s.alternateScreenActive = active
	s.alternateScreenTail = trailingBytes(scan, maxAlternateScreenSequenceLen-1)
}

func (s *session) appendOutputBufferLocked(chunk []byte) {
	s.outputBuffer = append(s.outputBuffer, chunk...)
	if extra := len(s.outputBuffer) - maxSessionOutputReplay; extra > 0 {
		s.outputBuffer = append([]byte(nil), s.outputBuffer[extra:]...)
	}
}

func alternateScreenEvents(data []byte) []alternateScreenEvent {
	events := make([]alternateScreenEvent, 0, 2)
	for i := range data {
		if seq, ok := matchingSequence(data[i:], alternateScreenEnterSequences); ok {
			events = append(events, alternateScreenEvent{
				start:  i,
				end:    i + len(seq),
				active: true,
			})
			continue
		}
		if seq, ok := matchingSequence(data[i:], alternateScreenExitSequences); ok {
			events = append(events, alternateScreenEvent{
				start:  i,
				end:    i + len(seq),
				active: false,
			})
		}
	}
	return events
}

func matchingSequence(data []byte, sequences [][]byte) ([]byte, bool) {
	for _, seq := range sequences {
		if bytes.HasPrefix(data, seq) {
			return seq, true
		}
	}
	return nil, false
}

func trailingBytes(data []byte, maxLen int) []byte {
	if maxLen <= 0 || len(data) == 0 {
		return nil
	}
	if len(data) > maxLen {
		data = data[len(data)-maxLen:]
	}
	return slices.Clone(data)
}

func (s *session) subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 64)

	s.mu.Lock()
	info := s.info
	if len(s.outputBuffer) > 0 && !s.alternateScreenActive {
		replay := append([]byte(nil), s.outputBuffer...)
		ch <- replay
		slog.Debug(
			"runtime terminal replay queued",
			"workspace_id", info.WorkspaceID,
			"session_key", info.Key,
			"bytes", len(replay),
		)
	}
	if s.outputClosed {
		close(ch)
		s.mu.Unlock()
		return ch, func() {}
	}
	s.subscribers[ch] = struct{}{}
	subscriberCount := len(s.subscribers)
	s.mu.Unlock()
	slog.Debug(
		"runtime terminal subscriber added",
		"workspace_id", info.WorkspaceID,
		"session_key", info.Key,
		"subscribers", subscriberCount,
	)

	return ch, func() {
		s.mu.Lock()
		info := s.info
		if _, ok := s.subscribers[ch]; ok {
			delete(s.subscribers, ch)
			close(ch)
		}
		subscriberCount := len(s.subscribers)
		s.mu.Unlock()
		slog.Debug(
			"runtime terminal subscriber removed",
			"workspace_id", info.WorkspaceID,
			"session_key", info.Key,
			"subscribers", subscriberCount,
		)
	}
}

func (s *session) closeSubscribers() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.outputClosed {
		return
	}
	s.outputClosed = true
	for ch := range s.subscribers {
		delete(s.subscribers, ch)
		close(ch)
	}
}

func (s *session) stop() {
	s.stopOnce.Do(func() {
		if s.cmd.Process != nil {
			// pty.StartWithSize sets Setsid, so the launched
			// process is a session/pgid leader. Send SIGKILL to
			// -pid to reach every descendant in the group;
			// otherwise an agent's detached children would
			// outlive the session. Fall back to single-process
			// kill if the group call fails.
			if err := syscall.Kill(
				-s.cmd.Process.Pid, syscall.SIGKILL,
			); err != nil {
				_ = s.cmd.Process.Kill()
			}
		}
		if s.ptmx != nil {
			_ = s.ptmx.Close()
		}
	})
}

func (s *session) detach() {
	s.stopOnce.Do(func() {
		if s.ptmx != nil {
			_ = s.ptmx.Close()
		}
	})
}

func waitSessionDone(s *session) {
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
	}
}

func sessionKey(workspaceID string, targetKey string) string {
	return workspaceID + ":" + targetKey
}

// resolveExecutable returns an absolute path for name. Names that
// are already absolute are accepted as-is; names without a path
// separator are looked up via PATH; relative names with separators
// (e.g. "./agent", "scripts/codex") are rejected because cmd.Dir
// is set to the workspace worktree, which is PR-controlled content.
func resolveExecutable(name string) (string, error) {
	if name == "" {
		return "", errors.New("session command is empty")
	}
	if filepath.IsAbs(name) {
		return name, nil
	}
	if !strings.ContainsRune(name, filepath.Separator) {
		path, err := exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf(
				"resolve session command %q via PATH: %w",
				name, err,
			)
		}
		// LookPath joins the matched PATH entry with name; a
		// relative entry like "bin" or "." yields a relative
		// result, which would re-resolve inside cmd.Dir (the
		// worktree). Bind it to an absolute path now, while we
		// are still in middleman's working directory.
		if !filepath.IsAbs(path) {
			abs, err := filepath.Abs(path)
			if err != nil {
				return "", fmt.Errorf(
					"resolve session command %q via PATH: %w",
					name, err,
				)
			}
			path = abs
		}
		return path, nil
	}
	return "", fmt.Errorf(
		"session command %q must be an absolute path or a "+
			"PATH-resolvable name; relative paths resolve inside "+
			"the workspace worktree, which is untrusted",
		name,
	)
}

// sessionVarPrefixes name prefixes whose env vars are stripped from
// launched runtime sessions. These tend to carry server credentials
// or API keys that an agent process running inside an untrusted
// workspace must not be able to read.
var sessionVarPrefixes = []string{
	"MIDDLEMAN_GITHUB_TOKEN",
	"GITHUB_TOKEN",
	"GH_TOKEN",
	"GITHUB_PAT",
	"GH_PAT",
	"GITHUB_ENTERPRISE_TOKEN",
	"GH_ENTERPRISE_TOKEN",
}

// sessionEnvironment returns a copy of env with credential-shaped
// variables removed (matched by the built-in prefix list and any
// names in extraStrip). Other variables are preserved.
func sessionEnvironment(env []string, extraStrip []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			out = append(out, kv)
			continue
		}
		key := kv[:eq]
		if shouldStripSessionVar(key, extraStrip) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func shouldStripSessionVar(key string, extraStrip []string) bool {
	for _, prefix := range sessionVarPrefixes {
		if key == prefix || strings.HasPrefix(key, prefix+"_") {
			return true
		}
	}
	return slices.Contains(extraStrip, key)
}

func defaultShellCommand() []string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return []string{shell}
	}
	return []string{"/bin/sh"}
}

func waitExitCode(waitErr error) int {
	if waitErr == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func attachToSession(
	s *session,
	workspaceID string,
	key string,
) (*Attachment, error) {
	if s == nil {
		return nil, fmt.Errorf("session %q not found", key)
	}
	info := s.snapshot()
	if info.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("session %q not found", key)
	}
	if info.Status != SessionStatusRunning &&
		info.Status != SessionStatusStarting {
		return nil, fmt.Errorf("session %q is not running", key)
	}

	output, unsubscribe := s.subscribe()
	return &Attachment{
		Output: output,
		Done:   s.done,
		info:   s.snapshot,
		write: func(data []byte) error {
			_, err := s.ptmx.Write(data)
			return err
		},
		resize: func(cols, rows int) error {
			if cols <= 0 || rows <= 0 {
				return nil
			}
			return pty.Setsize(s.ptmx, &pty.Winsize{
				Rows: uint16(rows),
				Cols: uint16(cols),
			})
		},
		close: unsubscribe,
	}, nil
}
