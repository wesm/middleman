package workspace

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/gitclone"
	"github.com/wesm/middleman/internal/procutil"
)

// Manager handles workspace lifecycle: create, setup, delete.
type Manager struct {
	db          *db.DB
	worktreeDir string
	clones      *gitclone.Manager
	tmuxCmd     []string
	retryMu     sync.Mutex
	retryQueued map[string]bool
}

const (
	workspaceSetupStageSetup       = "setup"
	workspaceSetupStageClone       = "clone"
	workspaceSetupStageWorktree    = "worktree"
	workspaceSetupStageTmuxSession = "tmux_session"
	workspaceBranchUnknown         = "__middleman_unknown__"
)

var workspacePersistTimeout = 5 * time.Second
var workspaceCleanupTimeout = 5 * time.Second

var (
	ErrWorkspaceNotFound     = errors.New("workspace not found")
	ErrWorkspaceNotSynced    = errors.New("workspace merge request not synced")
	ErrWorkspaceDuplicate    = errors.New("workspace already exists")
	ErrWorkspaceInvalidState = errors.New("workspace invalid state")
)

// NewManager creates a Manager that stores worktrees under
// worktreeDir.
func NewManager(
	database *db.DB, worktreeDir string,
) *Manager {
	return &Manager{
		db:          database,
		worktreeDir: worktreeDir,
		retryQueued: make(map[string]bool),
	}
}

// SetClones sets the git clone manager used for bare clone
// operations. Called after the clone manager is initialized.
func (m *Manager) SetClones(clones *gitclone.Manager) {
	m.clones = clones
}

// SetTmuxCommand sets the command + argv prefix for every tmux
// invocation the manager issues. When nil/empty, the manager uses
// ["tmux"] — preserving today's behavior.
func (m *Manager) SetTmuxCommand(cmd []string) {
	m.tmuxCmd = append([]string(nil), cmd...)
}

// tmuxExec builds an *exec.Cmd for a tmux invocation: the
// configured prefix + extra args. Defaults to ["tmux"] when
// unconfigured. Returning the *exec.Cmd directly (rather than a
// []string that callers index) keeps the first-element access
// inside this function where the branch structure makes it
// statically safe — NilAway cannot prove safety through an indexed
// slice return.
func (m *Manager) tmuxExec(
	ctx context.Context, extra ...string,
) *exec.Cmd {
	if len(m.tmuxCmd) == 0 {
		return exec.CommandContext(ctx, "tmux", extra...)
	}
	args := make([]string, 0, len(m.tmuxCmd)-1+len(extra))
	args = append(args, m.tmuxCmd[1:]...)
	args = append(args, extra...)
	return exec.CommandContext(ctx, m.tmuxCmd[0], args...)
}

// Create validates inputs, inserts a workspace row with status
// "creating", and returns it. The caller runs Setup in the
// background.
func (m *Manager) Create(
	ctx context.Context,
	platformHost, owner, name string,
	mrNumber int,
) (*Workspace, error) {
	repo, err := m.db.GetRepoByHostOwnerName(
		ctx, platformHost, owner, name,
	)
	if err != nil {
		return nil, fmt.Errorf("look up repo: %w", err)
	}
	if repo == nil {
		return nil, fmt.Errorf("%w: repository not tracked", ErrWorkspaceNotFound)
	}

	mr, err := m.db.GetMergeRequestByRepoIDAndNumber(
		ctx, repo.ID, mrNumber,
	)
	if err != nil {
		return nil, fmt.Errorf("look up merge request: %w", err)
	}
	if mr == nil {
		return nil, fmt.Errorf(
			"%w: merge request %d", ErrWorkspaceNotSynced, mrNumber,
		)
	}

	idBytes := make([]byte, 8)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("generate workspace id: %w", err)
	}
	id := hex.EncodeToString(idBytes)

	wtPath := filepath.Join(
		m.worktreeDir, platformHost, owner, name,
		fmt.Sprintf("pr-%d", mrNumber),
	)

	var headRepo *string
	if mr.HeadRepoCloneURL != "" {
		s := mr.HeadRepoCloneURL
		headRepo = &s
	}

	ws := &Workspace{
		ID:              id,
		PlatformHost:    platformHost,
		RepoOwner:       owner,
		RepoName:        name,
		MRNumber:        mrNumber,
		MRHeadRef:       mr.HeadBranch,
		MRHeadRepo:      headRepo,
		WorkspaceBranch: workspaceBranchUnknown,
		WorktreePath:    wtPath,
		TmuxSession:     "middleman-" + id,
		Status:          "creating",
	}

	if err := m.db.InsertWorkspace(ctx, ws); err != nil {
		if isUniqueConstraintError(err) {
			return nil, fmt.Errorf("%w: %v", ErrWorkspaceDuplicate, err)
		}
		return nil, fmt.Errorf("insert workspace: %w", err)
	}
	return ws, nil
}

// Setup clones/fetches the repo, creates the git worktree, starts
// a tmux session, and marks the workspace "ready". On failure it
// rolls back the worktree and sets status to "error".
func (m *Manager) Setup(
	ctx context.Context, ws *Workspace,
) error {
	m.recordSetupEvent(
		ctx,
		ws.ID, workspaceSetupStageSetup, "started",
		"starting workspace setup",
	)
	if m.clones == nil {
		return m.failSetup(
			ctx,
			ws.ID, workspaceSetupStageClone,
			fmt.Errorf("clone manager not set"),
		)
	}

	remoteURL := fmt.Sprintf(
		"https://%s/%s/%s.git",
		ws.PlatformHost, ws.RepoOwner, ws.RepoName,
	)

	if err := m.clones.EnsureClone(
		ctx, ws.PlatformHost, ws.RepoOwner,
		ws.RepoName, remoteURL,
	); err != nil {
		return m.failSetup(
			ctx,
			ws.ID, workspaceSetupStageClone, err,
		)
	}

	cloneDir := m.clones.ClonePath(
		ws.PlatformHost, ws.RepoOwner, ws.RepoName,
	)

	branch, err := m.addWorktree(ctx, cloneDir, ws)
	if err != nil {
		return m.failSetup(
			ctx,
			ws.ID, workspaceSetupStageWorktree, err,
		)
	}
	ws.WorkspaceBranch = branch
	if err := m.updateWorkspaceBranch(
		ctx, ws.ID, branch,
	); err != nil {
		m.rollbackWorktree(ctx, cloneDir, ws, branch)
		return m.failSetup(
			ctx,
			ws.ID, workspaceSetupStageWorktree, err,
		)
	}

	err = m.newTmuxSession(ctx, ws.TmuxSession, ws.WorktreePath)
	if err != nil {
		m.rollbackWorktree(ctx, cloneDir, ws, branch)
		return m.failSetup(
			ctx,
			ws.ID, workspaceSetupStageTmuxSession, err,
		)
	}
	m.recordSetupEvent(
		ctx,
		ws.ID, workspaceSetupStageTmuxSession, "success",
		"tmux session started",
	)

	if err := m.updateWorkspaceStatus(
		ctx, ws.ID, "ready", nil,
	); err != nil {
		return m.failSetup(
			ctx,
			ws.ID, workspaceSetupStageSetup,
			fmt.Errorf("update status to ready: %w", err),
		)
	}
	m.recordSetupEvent(
		ctx,
		ws.ID, workspaceSetupStageSetup, "ready",
		"workspace ready",
	)
	return nil
}

func (m *Manager) addWorktree(
	ctx context.Context, cloneDir string, ws *Workspace,
) (string, error) {
	if branch, err := m.addPreferredWorktree(ctx, cloneDir, ws); err == nil {
		return branch, nil
	} else {
		fallbackBranch := syntheticWorktreeBranch(ws.MRNumber)
		startRef := workspaceStartRef(ws)
		fallbackErr := runGit(
			ctx, cloneDir,
			"worktree", "add", ws.WorktreePath,
			"-b", fallbackBranch, startRef,
		)
		if fallbackErr == nil {
			return fallbackBranch, nil
		}
		return "", fmt.Errorf(
			"preferred branch %q failed: %w; fallback branch %q failed: %w",
			ws.MRHeadRef, err, fallbackBranch, fallbackErr,
		)
	}
}

func (m *Manager) addPreferredWorktree(
	ctx context.Context, cloneDir string, ws *Workspace,
) (string, error) {
	if err := validateLocalBranchName(
		ctx, cloneDir, ws.MRHeadRef,
	); err != nil {
		return "", err
	}

	if ws.MRHeadRepo != nil {
		err := runGit(
			ctx, cloneDir,
			"worktree", "add", ws.WorktreePath,
			"-b", ws.MRHeadRef, workspaceStartRef(ws),
		)
		if err != nil {
			return "", err
		}
		return ws.MRHeadRef, nil
	}

	startRef := workspaceStartRef(ws)
	startSHA, ok, err := gitRefSHA(ctx, cloneDir, startRef)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("start ref %q not found", startRef)
	}

	branchRef := "refs/heads/" + ws.MRHeadRef
	branchSHA, exists, err := gitRefSHA(ctx, cloneDir, branchRef)
	if err != nil {
		return "", err
	}
	if !exists {
		if err := runGit(
			ctx, cloneDir,
			"worktree", "add", ws.WorktreePath,
			"-b", ws.MRHeadRef, startRef,
		); err != nil {
			return "", err
		}
		if err := setBranchUpstream(
			ctx, ws.WorktreePath, ws.MRHeadRef,
			"origin", "refs/heads/"+ws.MRHeadRef,
		); err != nil {
			cleanupCtx, cancel := cleanupContext(ctx)
			defer cancel()
			_ = runGit(
				cleanupCtx, cloneDir,
				"worktree", "remove", "--force", ws.WorktreePath,
			)
			_ = runGit(
				cleanupCtx, cloneDir,
				"branch", "-D", "--", ws.MRHeadRef,
			)
			return "", fmt.Errorf("configure branch upstream: %w", err)
		}
		return ws.MRHeadRef, nil
	}
	if branchSHA != startSHA {
		return "", fmt.Errorf(
			"preferred branch %q points at %s, not %s",
			ws.MRHeadRef, branchSHA, startSHA,
		)
	}

	if err := runGit(
		ctx, cloneDir,
		"worktree", "add", ws.WorktreePath, ws.MRHeadRef,
	); err != nil {
		return "", err
	}

	if err := setBranchUpstream(
		ctx, ws.WorktreePath, ws.MRHeadRef,
		"origin", "refs/heads/"+ws.MRHeadRef,
	); err != nil {
		cleanupCtx, cancel := cleanupContext(ctx)
		defer cancel()
		_ = runGit(
			cleanupCtx, cloneDir,
			"worktree", "remove", "--force", ws.WorktreePath,
		)
		return "", fmt.Errorf("configure branch upstream: %w", err)
	}

	return "", nil
}

func workspaceStartRef(ws *Workspace) string {
	if ws.MRHeadRepo != nil {
		return fmt.Sprintf("refs/pull/%d/head", ws.MRNumber)
	}
	return "origin/" + ws.MRHeadRef
}

func syntheticWorktreeBranch(mrNumber int) string {
	return fmt.Sprintf("middleman/pr-%d", mrNumber)
}

func setBranchUpstream(
	ctx context.Context,
	worktreePath, branch, remote, mergeRef string,
) error {
	if err := runGit(
		ctx, worktreePath,
		"config", "branch."+branch+".remote", remote,
	); err != nil {
		return err
	}
	return runGit(
		ctx, worktreePath,
		"config", "branch."+branch+".merge", mergeRef,
	)
}

func validateLocalBranchName(
	ctx context.Context, dir, branch string,
) error {
	cmd := exec.CommandContext(
		ctx, "git", "check-ref-format", "--branch", branch,
	)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := procutil.CombinedOutput(
		ctx, cmd, "git subprocess capacity",
	)
	if err == nil {
		return nil
	}

	msg := strings.TrimSpace(string(out))
	if msg == "" {
		msg = err.Error()
	}
	return fmt.Errorf("invalid branch name %q: %s", branch, msg)
}

// Delete tears down a workspace: kills tmux, removes the git
// worktree and branch, and deletes the DB record.
// If force is false and the worktree has uncommitted changes,
// it returns the dirty file list without deleting.
func (m *Manager) Delete(
	ctx context.Context, id string, force bool,
) (dirty []string, err error) {
	ws, err := m.db.GetWorkspace(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get workspace: %w", err)
	}
	if ws == nil {
		return nil, ErrWorkspaceNotFound
	}

	if !force {
		files, checkErr := dirtyFiles(ctx, ws.WorktreePath)
		if checkErr != nil {
			// Worktree may be missing/corrupt — surface as a
			// dirty-state response so the UI can offer force-delete.
			return []string{
				fmt.Sprintf("(dirty check failed: %v)", checkErr),
			}, nil
		}
		if len(files) > 0 {
			return files, nil
		}
	}

	if err := m.cleanupWorkspaceArtifactsForDelete(ctx, ws); err != nil {
		return nil, err
	}

	if err := m.db.DeleteWorkspace(ctx, id); err != nil {
		return nil, fmt.Errorf("delete workspace record: %w", err)
	}
	return nil, nil
}

// RequestRetry prepares an errored workspace for another setup
// attempt. If setup is already running, it queues one follow-up retry
// and returns startNow=false. If the workspace is not errored or
// creating, the request is discarded and startNow=false.
func (m *Manager) RequestRetry(
	ctx context.Context, id string,
) (*Workspace, bool, error) {
	ws, err := m.db.GetWorkspace(ctx, id)
	if err != nil {
		return nil, false, fmt.Errorf("get workspace: %w", err)
	}
	if ws == nil {
		return nil, false, ErrWorkspaceNotFound
	}
	started, err := m.db.StartWorkspaceRetry(ctx, ws.ID)
	if err != nil {
		return nil, false, err
	}
	if !started {
		return m.queueRetryOrStartErrored(ctx, id)
	}

	if err := m.prepareWorkspaceRetry(ctx, ws); err != nil {
		return nil, false, err
	}
	return ws, true, nil
}

// StartQueuedRetryIfErrored consumes one queued retry for id. It
// starts the retry only if the workspace is still in error status at
// the time the queue is consumed; otherwise the queued retry is
// discarded.
func (m *Manager) StartQueuedRetryIfErrored(
	ctx context.Context, id string,
) (*Workspace, bool, error) {
	if !m.consumeQueuedRetry(id) {
		return nil, false, nil
	}

	ws, err := m.db.GetWorkspace(ctx, id)
	if err != nil {
		return nil, false, fmt.Errorf("get workspace: %w", err)
	}
	if ws == nil || ws.Status != "error" {
		return ws, false, nil
	}

	started, err := m.db.StartWorkspaceRetry(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if !started {
		return ws, false, nil
	}

	if err := m.prepareWorkspaceRetry(ctx, ws); err != nil {
		return nil, false, err
	}
	return ws, true, nil
}

func (m *Manager) queueRetryOrStartErrored(
	ctx context.Context, id string,
) (*Workspace, bool, error) {
	// Serialize the status re-check with queue consumption. If setup
	// already failed and the worker drained an empty queue, the retry
	// request must start the next setup attempt itself.
	m.retryMu.Lock()
	current, getErr := m.db.GetWorkspace(ctx, id)
	if getErr != nil {
		m.retryMu.Unlock()
		return nil, false, fmt.Errorf(
			"get workspace after retry conflict: %w", getErr,
		)
	}
	if current == nil {
		m.retryMu.Unlock()
		return nil, false, ErrWorkspaceNotFound
	}
	switch current.Status {
	case "creating":
		m.retryQueued[id] = true
		m.retryMu.Unlock()
		return current, false, nil
	case "error":
		delete(m.retryQueued, id)
		m.retryMu.Unlock()
		return m.startWorkspaceRetry(ctx, current)
	default:
		m.retryMu.Unlock()
		return nil, false, fmt.Errorf(
			"%w: workspace is not in error status",
			ErrWorkspaceInvalidState,
		)
	}
}

func (m *Manager) startWorkspaceRetry(
	ctx context.Context, ws *Workspace,
) (*Workspace, bool, error) {
	started, err := m.db.StartWorkspaceRetry(ctx, ws.ID)
	if err != nil {
		return nil, false, err
	}
	if !started {
		return m.queueRetryOrStartErrored(ctx, ws.ID)
	}

	if err := m.prepareWorkspaceRetry(ctx, ws); err != nil {
		return nil, false, err
	}
	return ws, true, nil
}

func (m *Manager) prepareWorkspaceRetry(
	ctx context.Context, ws *Workspace,
) error {
	if err := m.cleanupWorkspaceArtifactsForRetry(ctx, ws); err != nil {
		return m.failSetup(
			ctx,
			ws.ID, workspaceSetupStageSetup,
			fmt.Errorf(
				"cleanup workspace artifacts before retry: %w", err,
			),
		)
	}
	if err := m.updateWorkspaceBranch(
		ctx, ws.ID, workspaceBranchUnknown,
	); err != nil {
		return m.failSetup(
			ctx,
			ws.ID, workspaceSetupStageSetup,
			fmt.Errorf("reset workspace branch before retry: %w", err),
		)
	}
	m.markRetryStarted(ctx, ws)
	return nil
}

func (m *Manager) consumeQueuedRetry(id string) bool {
	m.retryMu.Lock()
	defer m.retryMu.Unlock()
	if !m.retryQueued[id] {
		return false
	}
	delete(m.retryQueued, id)
	return true
}

func (m *Manager) markRetryStarted(ctx context.Context, ws *Workspace) {
	ws.WorkspaceBranch = workspaceBranchUnknown
	ws.Status = "creating"
	ws.ErrorMessage = nil
	m.recordSetupEvent(
		ctx,
		ws.ID, workspaceSetupStageSetup, "retrying",
		"retrying workspace setup",
	)
}

func (m *Manager) cleanupWorkspaceArtifactsForRetry(
	ctx context.Context, ws *Workspace,
) error {
	if err := m.cleanupTmuxSession(ctx, ws); err != nil {
		return err
	}

	if m.clones == nil {
		return nil
	}

	cloneDir := m.clones.ClonePath(
		ws.PlatformHost, ws.RepoOwner, ws.RepoName,
	)
	ready, err := gitCloneDirReady(cloneDir)
	if err != nil {
		return err
	}
	if !ready {
		return nil
	}

	if err := runGit(
		ctx, cloneDir,
		"worktree", "remove", "--force", ws.WorktreePath,
	); err != nil && !isGitWorktreeAbsent(err) {
		return fmt.Errorf("remove git worktree: %w", err)
	}
	if err := m.deleteWorkspaceBranchesStrict(
		ctx, cloneDir, ws, ws.WorkspaceBranch,
	); err != nil {
		return err
	}
	if err := runGit(ctx, cloneDir, "worktree", "prune"); err != nil {
		return fmt.Errorf("prune git worktrees: %w", err)
	}
	return nil
}

func (m *Manager) cleanupWorkspaceArtifactsForDelete(
	ctx context.Context, ws *Workspace,
) error {
	if err := m.cleanupTmuxSession(ctx, ws); err != nil {
		return err
	}

	if m.clones == nil {
		return nil
	}

	cloneDir := m.clones.ClonePath(
		ws.PlatformHost, ws.RepoOwner, ws.RepoName,
	)

	_ = runGit(
		ctx, cloneDir,
		"worktree", "remove", "--force", ws.WorktreePath,
	)
	m.deleteWorkspaceBranches(ctx, cloneDir, ws, ws.WorkspaceBranch)
	_ = runGit(ctx, cloneDir, "worktree", "prune")
	return nil
}

func (m *Manager) cleanupTmuxSession(
	ctx context.Context, ws *Workspace,
) error {
	if err := m.killTmuxSession(ctx, ws.TmuxSession); err != nil &&
		!isTmuxSessionAbsent([]byte(err.Error()), err) {
		hasSession, checkErr := m.workspaceHasCreatedTmuxSession(ctx, ws)
		if checkErr != nil {
			return checkErr
		}
		if hasSession {
			return fmt.Errorf("kill tmux session: %w", err)
		}
	}
	return nil
}

// Get returns a workspace by ID, or nil if not found.
func (m *Manager) Get(
	ctx context.Context, id string,
) (*Workspace, error) {
	return m.db.GetWorkspace(ctx, id)
}

// GetByMR returns the workspace for a specific MR, or nil.
func (m *Manager) GetByMR(
	ctx context.Context,
	platformHost, owner, name string,
	mrNumber int,
) (*Workspace, error) {
	return m.db.GetWorkspaceByMR(
		ctx, platformHost, owner, name, mrNumber,
	)
}

// GetSummary returns a workspace with joined MR metadata.
func (m *Manager) GetSummary(
	ctx context.Context, id string,
) (*WorkspaceSummary, error) {
	return m.db.GetWorkspaceSummary(ctx, id)
}

// ListSummaries returns all workspaces with joined MR metadata.
func (m *Manager) ListSummaries(
	ctx context.Context,
) ([]WorkspaceSummary, error) {
	return m.db.ListWorkspaceSummaries(ctx)
}

// ReapOrphanTmuxSessions kills middleman-managed tmux sessions that no longer
// correspond to any workspace row. This is a conservative startup cleanup for
// stale sessions left behind by crashes or previous bugs.
func (m *Manager) ReapOrphanTmuxSessions(ctx context.Context) error {
	workspaces, err := m.db.ListWorkspaces(ctx)
	if err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}
	live := make(map[string]bool, len(workspaces))
	for _, ws := range workspaces {
		live[ws.TmuxSession] = true
	}

	sessions, err := m.listTmuxSessions(ctx)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if !isWorkspaceTmuxSessionName(session) {
			continue
		}
		if live[session] {
			continue
		}
		owned, err := m.tmuxSessionOwnedByThisManager(ctx, session)
		if err != nil {
			return err
		}
		if !owned {
			continue
		}
		if err := m.killTmuxSession(ctx, session); err != nil &&
			!isTmuxSessionAbsent([]byte(err.Error()), err) {
			return fmt.Errorf(
				"kill orphan tmux session %q: %w", session, err,
			)
		}
	}
	return nil
}

func isWorkspaceTmuxSessionName(session string) bool {
	const prefix = "middleman-"
	if len(session) != len(prefix)+16 ||
		!strings.HasPrefix(session, prefix) {
		return false
	}
	for _, ch := range session[len(prefix):] {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func (m *Manager) tmuxOwnerMarker() string {
	abs, err := filepath.Abs(m.worktreeDir)
	if err != nil {
		abs = m.worktreeDir
	}
	sum := sha256.Sum256([]byte(abs))
	return "middleman:" + hex.EncodeToString(sum[:8])
}

func (m *Manager) tmuxSessionOwnedByThisManager(
	ctx context.Context, session string,
) (bool, error) {
	cmd := m.tmuxExec(
		ctx,
		"show-options", "-qv", "-t", session,
		"@middleman_owner",
	)
	out, err := procutil.Output(
		ctx, cmd, "tmux subprocess capacity",
	)
	if err != nil {
		if procutil.IsResourceExhausted(err) {
			return false, err
		}
		return false, nil
	}
	return strings.TrimSpace(string(out)) == m.tmuxOwnerMarker(), nil
}

func (m *Manager) workspaceHasCreatedTmuxSession(
	ctx context.Context, ws *Workspace,
) (bool, error) {
	if ws.Status == "ready" {
		return true, nil
	}

	events, err := m.db.ListWorkspaceSetupEvents(ctx, ws.ID)
	if err != nil {
		return false, fmt.Errorf("list workspace setup events: %w", err)
	}
	for _, event := range events {
		if event.Stage == workspaceSetupStageTmuxSession &&
			event.Outcome == "success" {
			return true, nil
		}
		if event.Stage == workspaceSetupStageSetup &&
			event.Outcome == "ready" {
			return true, nil
		}
	}
	return false, nil
}

// EnsureTmux creates a tmux session if it does not already exist,
// using the manager's configured tmux command prefix.
func (m *Manager) EnsureTmux(
	ctx context.Context, session, cwd string,
) error {
	exists, err := m.tmuxSessionExists(ctx, session)
	if err != nil {
		return fmt.Errorf("tmux has-session: %w", err)
	}
	if exists {
		return nil
	}
	return m.newTmuxSession(ctx, session, cwd)
}

func (m *Manager) listTmuxSessions(
	ctx context.Context,
) ([]string, error) {
	cmd := m.tmuxExec(ctx, "list-sessions", "-F", "#{session_name}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := procutil.Run(ctx, cmd, "tmux subprocess capacity")
	if err != nil {
		if isTmuxSessionAbsent(stderr.Bytes(), err) {
			return nil, nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return nil, fmt.Errorf("tmux list-sessions: %w: %s", err, msg)
	}
	var sessions []string
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			sessions = append(sessions, line)
		}
	}
	return sessions, nil
}

func (m *Manager) newTmuxSession(
	ctx context.Context, session, cwd string,
) error {
	shell := userLoginShell()
	cmd := m.tmuxExec(
		ctx,
		"new-session", "-d",
		"-s", session,
		"-c", cwd,
		shell, "-l",
	)
	if err := runBuiltCmd(ctx, cmd); err != nil {
		return err
	}
	if err := m.setTmuxOwnerMarker(ctx, session); err != nil {
		if killErr := m.killTmuxSession(ctx, session); killErr != nil &&
			!isTmuxSessionAbsent([]byte(killErr.Error()), killErr) {
			return fmt.Errorf(
				"set tmux owner marker: %w; cleanup new tmux session: %v",
				err, killErr,
			)
		}
		return fmt.Errorf("set tmux owner marker: %w", err)
	}
	return nil
}

func (m *Manager) setTmuxOwnerMarker(
	ctx context.Context, session string,
) error {
	return runBuiltCmd(
		ctx,
		m.tmuxExec(
			ctx,
			"set-option", "-t", session,
			"@middleman_owner", m.tmuxOwnerMarker(),
		),
	)
}

// tmuxSessionExists runs `tmux has-session` and distinguishes a
// genuine "session absent" signal from a wrapper/binary failure.
// tmux reports session-absent by exiting 1 with one of two
// well-known stderr messages:
//
//	can't find session: <name>
//	no server running on <socket>
//
// Stdout and stderr are captured separately so a wrapper that
// happens to emit those phrases on stdout for unrelated reasons
// cannot masquerade as session-absent. Any other failure — missing
// binary (non-ExitError), wrapper exit codes other than 1, or
// exit-1 without the canonical stderr — propagates so
// misconfiguration surfaces instead of silently falling through to
// new-session through the same broken wrapper.
func (m *Manager) tmuxSessionExists(
	ctx context.Context, session string,
) (bool, error) {
	cmd := m.tmuxExec(ctx, "has-session", "-t", session)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := procutil.Run(ctx, cmd, "tmux subprocess capacity")
	if err == nil {
		return true, nil
	}
	if isTmuxSessionAbsent(stderr.Bytes(), err) {
		return false, nil
	}
	msg := strings.TrimSpace(stderr.String())
	if msg == "" {
		msg = strings.TrimSpace(stdout.String())
	}
	return false, fmt.Errorf("%w: %s", err, msg)
}

// isTmuxSessionAbsent reports whether a has-session failure is
// tmux's documented "session does not exist" signal. Must be both
// exit code 1 AND one of tmux's specific stderr phrases. Plain
// exit 1 is a common generic wrapper/shell failure code, and
// stdout content is not load-bearing — a wrapper could emit
// anything there for unrelated reasons.
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

// killTmuxSession kills a tmux session via the manager's prefix.
// Errors are returned rather than logged — callers decide whether
// to ignore them (Delete ignores; tests assert).
func (m *Manager) killTmuxSession(
	ctx context.Context, session string,
) error {
	return runBuiltCmd(
		ctx, m.tmuxExec(ctx, "kill-session", "-t", session),
	)
}

// userLoginShell resolves the current user's login shell from
// the OS user database (passwd), falling back to $SHELL, then
// /bin/sh.
func userLoginShell() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		if shell := lookupPasswdShell(u.Username); shell != "" {
			return shell
		}
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/sh"
}

func lookupPasswdShell(username string) string {
	cmd := exec.Command("getent", "passwd", username)
	out, err := procutil.Output(
		context.Background(), cmd, "shell lookup subprocess capacity",
	)
	if err == nil {
		return shellFromPasswdLine(string(out))
	}
	// Fallback: read /etc/passwd directly with exact field match
	// (no grep — avoids regex injection from metacharacters in
	// usernames).
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}
	prefix := username + ":"
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, prefix) {
			return shellFromPasswdLine(line)
		}
	}
	return ""
}

func shellFromPasswdLine(line string) string {
	line = strings.TrimSpace(line)
	fields := strings.Split(line, ":")
	if len(fields) < 7 {
		return ""
	}
	shell := strings.TrimSpace(fields[len(fields)-1])
	if shell == "" || shell == "/usr/bin/false" ||
		shell == "/bin/false" || shell == "/sbin/nologin" {
		return ""
	}
	return shell
}

// runGit executes a git command in dir and returns combined
// output on error.
func runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := procutil.CombinedOutput(
		ctx, cmd, "git subprocess capacity",
	)
	if err != nil {
		return fmt.Errorf(
			"%w: %s", err, strings.TrimSpace(string(out)),
		)
	}
	return nil
}

// runBuiltCmd runs a pre-built exec.Cmd and wraps any failure with
// the combined output. Used for tmux invocations whose *exec.Cmd is
// assembled by tmuxExec so argv[0] access stays inside that helper.
func runBuiltCmd(ctx context.Context, cmd *exec.Cmd) error {
	out, err := procutil.CombinedOutput(
		ctx, cmd, "tmux subprocess capacity",
	)
	if err != nil {
		return fmt.Errorf(
			"%w: %s", err, strings.TrimSpace(string(out)),
		)
	}
	return nil
}

// dirtyFiles returns the list of uncommitted files in a worktree.
func dirtyFiles(
	ctx context.Context, worktreePath string,
) ([]string, error) {
	cmd := exec.CommandContext(
		ctx, "git", "-C", worktreePath,
		"status", "--porcelain",
	)
	out, err := procutil.Output(
		ctx, cmd, "git subprocess capacity",
	)
	if err != nil {
		return nil, err
	}
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return nil, nil
	}
	var files []string
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			// porcelain format: "XY filename"
			if len(line) > 3 {
				files = append(files, line[3:])
			} else {
				files = append(files, line)
			}
		}
	}
	return files, nil
}

func (m *Manager) setErrorWithContext(
	ctx context.Context, id string, origErr error,
) {
	msg := origErr.Error()
	if err := m.updateWorkspaceStatusWithContext(
		ctx, id, "error", &msg,
	); err != nil {
		slog.Error("failed to set workspace error status",
			"workspace_id", id, "err", err)
	}
}

func (m *Manager) recordSetupEvent(
	ctx context.Context,
	workspaceID, stage, outcome, message string,
) {
	persistCtx, cancel := m.persistenceContext(ctx)
	defer cancel()
	m.recordSetupEventWithContext(
		persistCtx, workspaceID, stage, outcome, message,
	)
}

func (m *Manager) recordSetupEventWithContext(
	ctx context.Context,
	workspaceID, stage, outcome, message string,
) {
	err := m.db.InsertWorkspaceSetupEvent(
		ctx,
		&db.WorkspaceSetupEvent{
			WorkspaceID: workspaceID,
			Stage:       stage,
			Outcome:     outcome,
			Message:     message,
		},
	)
	if err != nil {
		slog.Warn("workspace setup audit insert failed",
			"workspace_id", workspaceID,
			"stage", stage,
			"outcome", outcome,
			"err", err,
		)
	}
}

func (m *Manager) failSetup(
	ctx context.Context,
	workspaceID, stage string, origErr error,
) error {
	wrapped := wrapWorkspaceSetupError(stage, origErr)
	persistCtx, cancel := m.persistenceContext(ctx)
	defer cancel()
	m.recordSetupEventWithContext(
		persistCtx, workspaceID, stage, "failure", wrapped.Error(),
	)
	slog.Error("workspace setup failed",
		"workspace_id", workspaceID,
		"stage", stage,
		"err", wrapped,
	)
	m.setErrorWithContext(persistCtx, workspaceID, wrapped)
	return wrapped
}

func wrapWorkspaceSetupError(stage string, err error) error {
	if procutil.IsResourceExhausted(err) {
		switch stage {
		case workspaceSetupStageClone:
			return fmt.Errorf(
				"ensure clone: host process limit reached while starting git or helper processes: %w",
				err,
			)
		case workspaceSetupStageWorktree:
			return fmt.Errorf(
				"add git worktree: host process limit reached while starting git or helper processes: %w",
				err,
			)
		case workspaceSetupStageTmuxSession:
			return fmt.Errorf(
				"tmux new-session: host process limit reached while starting tmux or shell: %w",
				err,
			)
		}
	}
	switch stage {
	case workspaceSetupStageClone:
		return fmt.Errorf("ensure clone: %w", err)
	case workspaceSetupStageWorktree:
		return fmt.Errorf("add git worktree: %w", err)
	case workspaceSetupStageTmuxSession:
		return fmt.Errorf("tmux new-session: %w", err)
	default:
		return err
	}
}

// rollbackWorktree removes a partially created worktree and its
// branch.
func (m *Manager) rollbackWorktree(
	ctx context.Context, cloneDir string, ws *Workspace,
	branch string,
) {
	cleanupCtx, cancel := cleanupContext(ctx)
	defer cancel()
	if err := runGit(
		cleanupCtx, cloneDir,
		"worktree", "remove", "--force", ws.WorktreePath,
	); err != nil {
		slog.Warn("rollback: worktree remove failed",
			"path", ws.WorktreePath, "err", err)
	}
	m.deleteWorkspaceBranches(cleanupCtx, cloneDir, ws, branch)
}

func (m *Manager) deleteWorkspaceBranches(
	ctx context.Context, cloneDir string, ws *Workspace,
	managedBranch string,
) {
	for _, branch := range workspaceBranchCandidates(ws, managedBranch) {
		if err := validateLocalBranchName(
			ctx, cloneDir, branch,
		); err != nil {
			slog.Warn("workspace branch delete skipped",
				"branch", branch, "err", err)
			continue
		}
		if err := runGit(
			ctx, cloneDir, "branch", "-D", "--", branch,
		); err != nil {
			slog.Warn("workspace branch delete failed",
				"branch", branch, "err", err)
		}
	}
}

func (m *Manager) deleteWorkspaceBranchesStrict(
	ctx context.Context, cloneDir string, ws *Workspace,
	managedBranch string,
) error {
	for _, branch := range workspaceBranchCandidates(ws, managedBranch) {
		if err := validateLocalBranchName(
			ctx, cloneDir, branch,
		); err != nil {
			return err
		}
		if err := runGit(
			ctx, cloneDir, "branch", "-D", "--", branch,
		); err != nil && !isGitBranchAbsent(err) {
			return fmt.Errorf("delete git branch %q: %w", branch, err)
		}
	}
	return nil
}

func isGitWorktreeAbsent(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "is not a working tree") ||
		strings.Contains(msg, "is not a worktree") ||
		strings.Contains(msg, "not a git repository") ||
		strings.Contains(msg, "no such file or directory")
}

func isGitBranchAbsent(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "branch") &&
		strings.Contains(msg, "not found")
}

func gitCloneDirReady(cloneDir string) (bool, error) {
	_, err := os.Stat(filepath.Join(cloneDir, "HEAD"))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("stat git clone dir: %w", err)
}

func isUniqueConstraintError(err error) bool {
	type sqliteCoder interface {
		Code() int
	}
	var coder sqliteCoder
	if !errors.As(err, &coder) {
		return false
	}
	const sqliteConstraintUnique = 2067
	return coder.Code() == sqliteConstraintUnique
}

func workspaceBranchCandidates(
	ws *Workspace, managedBranch string,
) []string {
	if managedBranch == workspaceBranchUnknown {
		return []string{syntheticWorktreeBranch(ws.MRNumber)}
	}
	if managedBranch == "" {
		return nil
	}
	return []string{managedBranch}
}

func (m *Manager) persistenceContext(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	return boundedDetachedContext(ctx, workspacePersistTimeout)
}

func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return boundedDetachedContext(ctx, workspaceCleanupTimeout)
}

func boundedDetachedContext(
	ctx context.Context, timeout time.Duration,
) (context.Context, context.CancelFunc) {
	base := context.WithoutCancel(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		if time.Until(deadline) <= timeout {
			return context.WithDeadline(base, deadline)
		}
	}
	return context.WithTimeout(base, timeout)
}

func (m *Manager) updateWorkspaceStatus(
	ctx context.Context, id, status string, errMsg *string,
) error {
	persistCtx, cancel := m.persistenceContext(ctx)
	defer cancel()
	return m.updateWorkspaceStatusWithContext(
		persistCtx, id, status, errMsg,
	)
}

func (m *Manager) updateWorkspaceStatusWithContext(
	ctx context.Context, id, status string, errMsg *string,
) error {
	return m.db.UpdateWorkspaceStatus(
		ctx, id, status, errMsg,
	)
}

func (m *Manager) updateWorkspaceBranch(
	ctx context.Context, id, branch string,
) error {
	persistCtx, cancel := m.persistenceContext(ctx)
	defer cancel()
	return m.db.UpdateWorkspaceBranch(
		persistCtx, id, branch,
	)
}

func gitRefSHA(
	ctx context.Context, dir, ref string,
) (string, bool, error) {
	cmd := exec.CommandContext(
		ctx, "git", "rev-parse", "--verify", "--quiet",
		ref+"^{commit}",
	)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := procutil.CombinedOutput(
		ctx, cmd, "git subprocess capacity",
	)
	if err == nil {
		return strings.TrimSpace(string(out)), true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return "", false, nil
	}
	return "", false, fmt.Errorf(
		"%w: %s", err, strings.TrimSpace(string(out)),
	)
}
