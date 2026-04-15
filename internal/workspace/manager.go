package workspace

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/gitclone"
)

// Manager handles workspace lifecycle: create, setup, delete.
type Manager struct {
	db          *db.DB
	worktreeDir string
	clones      *gitclone.Manager
}

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
		ID:           id,
		PlatformHost: platformHost,
		RepoOwner:    owner,
		RepoName:     name,
		MRNumber:     mrNumber,
		MRHeadRef:    mr.HeadBranch,
		MRHeadRepo:   headRepo,
		WorktreePath: wtPath,
		TmuxSession:  "middleman-" + id,
		Status:       "creating",
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
	if m.clones == nil {
		return fmt.Errorf("clone manager not set")
	}

	remoteURL := fmt.Sprintf(
		"https://%s/%s/%s.git",
		ws.PlatformHost, ws.RepoOwner, ws.RepoName,
	)

	if err := m.clones.EnsureClone(
		ctx, ws.PlatformHost, ws.RepoOwner,
		ws.RepoName, remoteURL,
	); err != nil {
		m.setError(ctx, ws.ID, err)
		return fmt.Errorf("ensure clone: %w", err)
	}

	branch := fmt.Sprintf("middleman/pr-%d", ws.MRNumber)
	var startRef string
	if ws.MRHeadRepo != nil {
		startRef = fmt.Sprintf(
			"refs/pull/%d/head", ws.MRNumber,
		)
	} else {
		startRef = "refs/heads/" + ws.MRHeadRef
	}

	cloneDir := m.clones.ClonePath(
		ws.PlatformHost, ws.RepoOwner, ws.RepoName,
	)

	err := runGit(
		ctx, cloneDir,
		"worktree", "add", ws.WorktreePath,
		"-b", branch, startRef,
	)
	if err != nil {
		m.setError(ctx, ws.ID, err)
		return fmt.Errorf("git worktree add: %w", err)
	}

	err = newTmuxSession(ctx, ws.TmuxSession, ws.WorktreePath)
	if err != nil {
		m.rollbackWorktree(ctx, cloneDir, ws)
		m.setError(ctx, ws.ID, err)
		return fmt.Errorf("tmux new-session: %w", err)
	}

	if err := m.db.UpdateWorkspaceStatus(
		ctx, ws.ID, "ready", nil,
	); err != nil {
		return fmt.Errorf("update status to ready: %w", err)
	}
	return nil
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
	_ = runCmd(
		ctx, "",
		"tmux", "kill-session", "-t", ws.TmuxSession,
	)

	if m.clones == nil {
		return nil, m.db.DeleteWorkspace(ctx, ws.ID)
	}

	cloneDir := m.clones.ClonePath(
		ws.PlatformHost, ws.RepoOwner, ws.RepoName,
	)
	branch := fmt.Sprintf("middleman/pr-%d", ws.MRNumber)

	// Remove worktree.
	_ = runGit(
		ctx, cloneDir,
		"worktree", "remove", "--force", ws.WorktreePath,
	)

	// Delete the tracking branch.
	_ = runGit(ctx, cloneDir, "branch", "-D", branch)

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

// EnsureTmux creates a tmux session if it does not already exist.
func EnsureTmux(
	ctx context.Context, session, cwd string,
) error {
	if TmuxSessionExists(ctx, session) {
		return nil
	}
	return newTmuxSession(ctx, session, cwd)
}

// newTmuxSession creates a detached tmux session that runs
// the user's default login shell. Without an explicit shell
// command, tmux uses its default-shell which may not source
// the user's shell profile (.zshrc, .bashrc, etc.).
func newTmuxSession(
	ctx context.Context, session, cwd string,
) error {
	shell := userLoginShell()
	return runCmd(
		ctx, "",
		"tmux", "new-session", "-d",
		"-s", session,
		"-c", cwd,
		shell, "-l",
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

// TmuxSessionExists checks whether a tmux session exists.
func TmuxSessionExists(
	ctx context.Context, session string,
) bool {
	err := runCmd(
		ctx, "",
		"tmux", "has-session", "-t", session,
	)
	return err == nil
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

// runCmd executes an arbitrary command. If dir is non-empty it
// sets the working directory.
func runCmd(
	ctx context.Context, dir string, name string, args ...string,
) error {
	cmd := exec.CommandContext(ctx, name, args...)
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
	ctx context.Context, id string, origErr error,
) {
	msg := origErr.Error()
	if err := m.db.UpdateWorkspaceStatus(
		ctx, id, "error", &msg,
	); err != nil {
		slog.Error("failed to set workspace error status",
			"workspace_id", id, "err", err)
	}
}

// rollbackWorktree removes a partially created worktree and its
// branch.
func (m *Manager) rollbackWorktree(
	ctx context.Context, cloneDir string, ws *Workspace,
) {
	branch := fmt.Sprintf("middleman/pr-%d", ws.MRNumber)
	if err := runGit(
		ctx, cloneDir,
		"worktree", "remove", "--force", ws.WorktreePath,
	); err != nil {
		slog.Warn("rollback: worktree remove failed",
			"path", ws.WorktreePath, "err", err)
	}
	if err := runGit(
		ctx, cloneDir, "branch", "-D", branch,
	); err != nil {
		slog.Warn("rollback: branch delete failed",
			"branch", branch, "err", err)
	}
}
