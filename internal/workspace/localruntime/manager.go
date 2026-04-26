package localruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
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

var errManagerShutdown = errors.New("runtime manager is shut down")

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
}

type Options struct {
	Targets      []LaunchTarget
	ShellCommand []string
	// StripEnvVars lists additional environment variable names that
	// must be removed from launched runtime sessions in addition to
	// the built-in credential prefixes. Pass the maintainer's
	// configured GitHub token env (and any per-repo overrides) so a
	// non-default name is also stripped.
	StripEnvVars []string
}

type Manager struct {
	mu           sync.Mutex
	targets      map[string]LaunchTarget
	targetsList  []LaunchTarget
	sessions     map[string]*session
	shells       map[string]*session
	shellCommand []string
	stripEnvVars []string
	startLocks   map[string]*sync.Mutex
	startWG      sync.WaitGroup
	closed       bool
}

// maxSessionOutputReplay caps how many bytes of recent PTY output
// the session retains for replay when a new subscriber attaches.
// Sized to comfortably hold an agent boot banner plus the first
// prompt — without it, a fast subscribe-after-launch flow can miss
// startup output entirely.
const maxSessionOutputReplay = 64 * 1024

type session struct {
	mu           sync.Mutex
	info         SessionInfo
	cmd          *exec.Cmd
	ptmx         *os.File
	done         chan struct{}
	subscribers  map[chan []byte]struct{}
	outputBuffer []byte
	outputClosed bool
	stopOnce     sync.Once
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
		targets:      targets,
		targetsList:  targetsList,
		sessions:     make(map[string]*session),
		shells:       make(map[string]*session),
		shellCommand: append([]string(nil), options.ShellCommand...),
		stripEnvVars: dedupeStrings(options.StripEnvVars),
		startLocks:   make(map[string]*sync.Mutex),
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

func (m *Manager) Launch(
	ctx context.Context,
	workspaceID string,
	cwd string,
	targetKey string,
) (SessionInfo, error) {
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
		return existing.snapshot(), nil
	}

	if err := m.beginStart(); err != nil {
		return SessionInfo{}, err
	}
	defer m.finishStart()

	started, err := startSession(SessionInfo{
		Key:         key,
		WorkspaceID: workspaceID,
		TargetKey:   targetKey,
		Label:       target.Label,
		Kind:        target.Kind,
		Status:      SessionStatusStarting,
		CreatedAt:   time.Now().UTC(),
	}, target.Command, cwd, m.stripEnvVars)
	if err != nil {
		return SessionInfo{}, err
	}
	go started.watch()

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		started.stop()
		waitSessionDone(started)
		return SessionInfo{}, errManagerShutdown
	}
	m.sessions[key] = started
	m.mu.Unlock()

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

func (m *Manager) Stop(
	ctx context.Context,
	workspaceID string,
	sessionKey string,
) error {
	s, ok := m.remove(workspaceID, sessionKey)
	if !ok {
		return fmt.Errorf("session %q not found", sessionKey)
	}

	s.stop()
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
		s.stop()
	}
	for _, s := range stopping {
		select {
		case <-s.done:
		case <-ctx.Done():
			return
		}
	}
}

func (m *Manager) AttachSession(
	workspaceID string,
	key string,
) (*Attachment, error) {
	m.mu.Lock()
	s := m.sessions[key]
	m.mu.Unlock()
	return attachToSession(s, workspaceID, key)
}

func (m *Manager) AttachShell(
	workspaceID string,
) (*Attachment, error) {
	key := sessionKey(workspaceID, "shell")
	m.mu.Lock()
	s := m.shells[key]
	m.mu.Unlock()
	return attachToSession(s, workspaceID, key)
}

func (m *Manager) EnsureShell(
	ctx context.Context,
	workspaceID string,
	cwd string,
) (SessionInfo, error) {
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
		return SessionInfo{}, err
	}
	go started.watch()

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		started.stop()
		waitSessionDone(started)
		return SessionInfo{}, errManagerShutdown
	}
	m.shells[key] = started
	m.mu.Unlock()

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

	for _, s := range sessions {
		s.stop()
	}
	for _, s := range sessions {
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

func (m *Manager) remove(
	workspaceID string,
	key string,
) (*session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[key]; ok &&
		s.snapshot().WorkspaceID == workspaceID {
		delete(m.sessions, key)
		return s, true
	}
	if s, ok := m.shells[key]; ok &&
		s.snapshot().WorkspaceID == workspaceID {
		delete(m.shells, key)
		return s, true
	}
	return nil, false
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
	s.mu.Unlock()

	_ = s.ptmx.Close()
	close(s.done)
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

	s.outputBuffer = append(s.outputBuffer, chunk...)
	if extra := len(s.outputBuffer) - maxSessionOutputReplay; extra > 0 {
		s.outputBuffer = append([]byte(nil), s.outputBuffer[extra:]...)
	}

	for ch := range s.subscribers {
		select {
		case ch <- chunk:
		default:
			delete(s.subscribers, ch)
			close(ch)
		}
	}
}

func (s *session) subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 64)

	s.mu.Lock()
	if len(s.outputBuffer) > 0 {
		replay := append([]byte(nil), s.outputBuffer...)
		ch <- replay
	}
	if s.outputClosed {
		close(ch)
		s.mu.Unlock()
		return ch, func() {}
	}
	s.subscribers[ch] = struct{}{}
	s.mu.Unlock()

	return ch, func() {
		s.mu.Lock()
		if _, ok := s.subscribers[ch]; ok {
			delete(s.subscribers, ch)
			close(ch)
		}
		s.mu.Unlock()
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
			_ = s.cmd.Process.Kill()
		}
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
// variables removed. The extraStrip list contains caller-provided
// variable names (e.g. the configured GitHub token env name and any
// per-repo overrides) that must also be stripped so a non-default
// token name is not exposed to PR-controlled session processes.
// Other variables are preserved so that the user's shell, language
// toolchains, and editors continue to work.
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
