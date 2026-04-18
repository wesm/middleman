package workspace

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/gitclone"
)

// Manager handles workspace lifecycle: create, setup, delete.
type Manager struct {
	db          *db.DB
	worktreeDir string
	clones      *gitclone.Manager
	tmuxCmd     []string
}

const (
	workspaceSetupStageSetup       = "setup"
	workspaceSetupStageClone       = "clone"
	workspaceSetupStageWorktree    = "worktree"
	workspaceSetupStageTmuxSession = "tmux_session"
	workspaceBranchUnknown         = "__middleman_unknown__"
	workspacePersistTimeout        = 5 * time.Second
)

// NewManager creates a Manager that stores worktrees under
// worktreeDir.
func NewManager(
	database *db.DB, worktreeDir string,
) *Manager {
	return &Manager{
		db:          database,
		worktreeDir: worktreeDir,
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
		return nil, fmt.Errorf("repository not tracked")
	}

	mr, err := m.db.GetMergeRequestByRepoIDAndNumber(
		ctx, repo.ID, mrNumber,
	)
	if err != nil {
		return nil, fmt.Errorf("look up merge request: %w", err)
	}
	if mr == nil {
		return nil, fmt.Errorf(
			"merge request %d not synced yet", mrNumber,
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
		ws.ID, workspaceSetupStageSetup, "started",
		"starting workspace setup",
	)
	if m.clones == nil {
		return m.failSetup(
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
			ws.ID, workspaceSetupStageClone, err,
		)
	}

	cloneDir := m.clones.ClonePath(
		ws.PlatformHost, ws.RepoOwner, ws.RepoName,
	)

	branch, err := m.addWorktree(ctx, cloneDir, ws)
	if err != nil {
		return m.failSetup(
			ws.ID, workspaceSetupStageWorktree, err,
		)
	}
	ws.WorkspaceBranch = branch
	if err := m.updateWorkspaceBranch(
		ws.ID, branch,
	); err != nil {
		m.rollbackWorktree(ctx, cloneDir, ws, branch)
		return m.failSetup(
			ws.ID, workspaceSetupStageWorktree, err,
		)
	}

	err = m.newTmuxSession(ctx, ws.TmuxSession, ws.WorktreePath)
	if err != nil {
		m.rollbackWorktree(ctx, cloneDir, ws, branch)
		return m.failSetup(
			ws.ID, workspaceSetupStageTmuxSession, err,
		)
	}

	if err := m.updateWorkspaceStatus(
		ws.ID, "ready", nil,
	); err != nil {
		return m.failSetup(
			ws.ID, workspaceSetupStageSetup,
			fmt.Errorf("update status to ready: %w", err),
		)
	}
	m.recordSetupEvent(
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
			_ = runGit(
				ctx, cloneDir,
				"worktree", "remove", "--force", ws.WorktreePath,
			)
			_ = runGit(
				ctx, cloneDir,
				"branch", "-D", ws.MRHeadRef,
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
		_ = runGit(
			ctx, cloneDir,
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
		return nil, fmt.Errorf("workspace not found")
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

	// Kill tmux session (ignore errors -- session may not exist).
	_ = m.killTmuxSession(ctx, ws.TmuxSession)

	if m.clones == nil {
		return nil, m.db.DeleteWorkspace(ctx, ws.ID)
	}

	cloneDir := m.clones.ClonePath(
		ws.PlatformHost, ws.RepoOwner, ws.RepoName,
	)

	// Remove worktree.
	_ = runGit(
		ctx, cloneDir,
		"worktree", "remove", "--force", ws.WorktreePath,
	)

	m.deleteWorkspaceBranches(ctx, cloneDir, ws, ws.WorkspaceBranch)

	// Prune stale worktree metadata.
	_ = runGit(ctx, cloneDir, "worktree", "prune")

	if err := m.db.DeleteWorkspace(ctx, id); err != nil {
		return nil, fmt.Errorf("delete workspace record: %w", err)
	}
	return nil, nil
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
	return runBuiltCmd(cmd)
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
	err := cmd.Run()
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
		strings.Contains(msg, "no server running")
}

// killTmuxSession kills a tmux session via the manager's prefix.
// Errors are returned rather than logged — callers decide whether
// to ignore them (Delete ignores; tests assert).
func (m *Manager) killTmuxSession(
	ctx context.Context, session string,
) error {
	return runBuiltCmd(m.tmuxExec(ctx, "kill-session", "-t", session))
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
	out, err := exec.Command("getent", "passwd", username).Output()
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
	out, err := cmd.CombinedOutput()
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
func runBuiltCmd(cmd *exec.Cmd) error {
	out, err := cmd.CombinedOutput()
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
	out, err := cmd.Output()
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

// setError marks a workspace as errored in the DB.
func (m *Manager) setError(
	id string, origErr error,
) {
	msg := origErr.Error()
	if err := m.updateWorkspaceStatus(
		id, "error", &msg,
	); err != nil {
		slog.Error("failed to set workspace error status",
			"workspace_id", id, "err", err)
	}
}

func (m *Manager) recordSetupEvent(
	workspaceID, stage, outcome, message string,
) {
	persistCtx, cancel := m.persistenceContext()
	defer cancel()

	err := m.db.InsertWorkspaceSetupEvent(
		persistCtx,
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
	workspaceID, stage string, origErr error,
) error {
	wrapped := wrapWorkspaceSetupError(stage, origErr)
	m.recordSetupEvent(
		workspaceID, stage, "failure", wrapped.Error(),
	)
	slog.Error("workspace setup failed",
		"workspace_id", workspaceID,
		"stage", stage,
		"err", wrapped,
	)
	m.setError(workspaceID, wrapped)
	return wrapped
}

func wrapWorkspaceSetupError(stage string, err error) error {
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
	if err := runGit(
		ctx, cloneDir,
		"worktree", "remove", "--force", ws.WorktreePath,
	); err != nil {
		slog.Warn("rollback: worktree remove failed",
			"path", ws.WorktreePath, "err", err)
	}
	m.deleteWorkspaceBranches(ctx, cloneDir, ws, branch)
}

func (m *Manager) deleteWorkspaceBranches(
	ctx context.Context, cloneDir string, ws *Workspace,
	managedBranch string,
) {
	for _, branch := range workspaceBranchCandidates(ws, managedBranch) {
		if err := runGit(
			ctx, cloneDir, "branch", "-D", branch,
		); err != nil {
			slog.Warn("workspace branch delete failed",
				"branch", branch, "err", err)
		}
	}
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

func (m *Manager) persistenceContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(
		context.Background(), workspacePersistTimeout,
	)
}

func (m *Manager) updateWorkspaceStatus(
	id, status string, errMsg *string,
) error {
	persistCtx, cancel := m.persistenceContext()
	defer cancel()
	return m.db.UpdateWorkspaceStatus(
		persistCtx, id, status, errMsg,
	)
}

func (m *Manager) updateWorkspaceBranch(
	id, branch string,
) error {
	persistCtx, cancel := m.persistenceContext()
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
	out, err := cmd.CombinedOutput()
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
