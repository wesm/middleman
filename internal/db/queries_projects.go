package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PlatformIdentity captures a project's optional VCS-platform identity. It is
// derived from the linked middleman_repos row at read time; middleman_projects
// itself stores only the FK in repo_id. A project may be registered without a
// linked repo (a local-only directory with no parseable remote), in which case
// PlatformIdentity is nil.
type PlatformIdentity struct {
	Platform string `json:"platform"`
	Host     string `json:"platform_host"`
	Owner    string `json:"owner"`
	Name     string `json:"name"`
}

// Project is the registry record for a local repository checkout middleman
// knows about. Identity (host/owner/name), when present, lives in
// middleman_repos and is joined in via repo_id.
type Project struct {
	ID               string
	DisplayName      string
	LocalPath        string
	PlatformIdentity *PlatformIdentity
	DefaultBranch    string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// ProjectWorktree is the registry record for a worktree of a Project. The
// caller performs the filesystem mutation (`git worktree add`); middleman only
// persists the metadata.
type ProjectWorktree struct {
	ID        string
	ProjectID string
	Branch    string
	Path      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ErrProjectNotFound is returned by GetProject* when no record matches.
var ErrProjectNotFound = errors.New("project not found")

// ErrProjectPathTaken is returned by CreateProject when local_path is already
// registered.
var ErrProjectPathTaken = errors.New("project local_path already registered")

// ErrWorktreePathTaken is returned by CreateProjectWorktree when path is
// already registered for any project.
var ErrWorktreePathTaken = errors.New("worktree path already registered")

// CreateProjectInput collects the fields required to register a new project.
// RepoID links the project to a middleman_repos row that owns the platform
// identity; pass an unset NullInt64 to register a local-only project.
type CreateProjectInput struct {
	DisplayName   string
	LocalPath     string
	RepoID        sql.NullInt64
	DefaultBranch string
}

// CreateProject persists a new project. The ID is generated server-side.
func (d *DB) CreateProject(ctx context.Context, in CreateProjectInput) (*Project, error) {
	displayName := strings.TrimSpace(in.DisplayName)
	if displayName == "" {
		return nil, fmt.Errorf("display_name is required")
	}
	localPath := strings.TrimSpace(in.LocalPath)
	if localPath == "" {
		return nil, fmt.Errorf("local_path is required")
	}

	id, err := newProjectID()
	if err != nil {
		return nil, err
	}

	defaultBranch := strings.TrimSpace(in.DefaultBranch)

	_, err = d.rw.ExecContext(ctx,
		`INSERT INTO middleman_projects (
		    id, display_name, local_path, repo_id, default_branch
		 ) VALUES (?, ?, ?, ?, ?)`,
		id, displayName, localPath, in.RepoID, defaultBranch,
	)
	if err != nil {
		if isUniqueConstraintErr(err, "middleman_projects.local_path") {
			return nil, ErrProjectPathTaken
		}
		return nil, fmt.Errorf("insert project: %w", err)
	}

	return d.GetProjectByID(ctx, id)
}

const projectSelectColumns = `p.id, p.display_name, p.local_path,
        p.default_branch, p.created_at, p.updated_at,
        r.platform, r.platform_host, r.owner, r.name`

const projectFromJoin = `FROM middleman_projects p
        LEFT JOIN middleman_repos r ON r.id = p.repo_id`

// GetProjectByID returns one project by its server-assigned id, joining the
// linked middleman_repos row to populate PlatformIdentity when present.
func (d *DB) GetProjectByID(ctx context.Context, id string) (*Project, error) {
	row := d.ro.QueryRowContext(ctx,
		`SELECT `+projectSelectColumns+` `+projectFromJoin+`
		 WHERE p.id = ?`,
		id,
	)
	return scanProject(row)
}

// GetProjectByLocalPath returns the project registered at the given absolute
// path, or ErrProjectNotFound if no record matches.
func (d *DB) GetProjectByLocalPath(ctx context.Context, localPath string) (*Project, error) {
	row := d.ro.QueryRowContext(ctx,
		`SELECT `+projectSelectColumns+` `+projectFromJoin+`
		 WHERE p.local_path = ?`,
		localPath,
	)
	return scanProject(row)
}

// ListProjects returns all registered projects ordered by display_name.
func (d *DB) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := d.ro.QueryContext(ctx,
		`SELECT `+projectSelectColumns+` `+projectFromJoin+`
		 ORDER BY p.display_name COLLATE NOCASE, p.id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		project, err := scanProjectRow(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, *project)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects: %w", err)
	}
	return projects, nil
}

// CreateProjectWorktreeInput collects fields for registering a worktree the
// caller has already created on disk.
type CreateProjectWorktreeInput struct {
	ProjectID string
	Branch    string
	Path      string
}

// CreateProjectWorktree persists a worktree record. The caller must have
// already run `git worktree add`; middleman only records metadata.
func (d *DB) CreateProjectWorktree(ctx context.Context, in CreateProjectWorktreeInput) (*ProjectWorktree, error) {
	projectID := strings.TrimSpace(in.ProjectID)
	if projectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	branch := strings.TrimSpace(in.Branch)
	if branch == "" {
		return nil, fmt.Errorf("branch is required")
	}
	path := strings.TrimSpace(in.Path)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	if _, err := d.GetProjectByID(ctx, projectID); err != nil {
		return nil, err
	}

	id, err := newWorktreeID()
	if err != nil {
		return nil, err
	}

	_, err = d.rw.ExecContext(ctx,
		`INSERT INTO middleman_project_worktrees (
		    id, project_id, branch, path
		 ) VALUES (?, ?, ?, ?)`,
		id, projectID, branch, path,
	)
	if err != nil {
		if isUniqueConstraintErr(err, "middleman_project_worktrees.path") {
			return nil, ErrWorktreePathTaken
		}
		return nil, fmt.Errorf("insert project worktree: %w", err)
	}

	return d.GetProjectWorktreeByID(ctx, id)
}

// GetProjectWorktreeByID returns one worktree by id.
func (d *DB) GetProjectWorktreeByID(ctx context.Context, id string) (*ProjectWorktree, error) {
	row := d.ro.QueryRowContext(ctx,
		`SELECT id, project_id, branch, path, created_at, updated_at
		 FROM middleman_project_worktrees WHERE id = ?`,
		id,
	)
	return scanProjectWorktree(row)
}

// ListProjectWorktrees returns the worktrees for a project ordered by
// created_at.
func (d *DB) ListProjectWorktrees(ctx context.Context, projectID string) ([]ProjectWorktree, error) {
	if _, err := d.GetProjectByID(ctx, projectID); err != nil {
		return nil, err
	}
	rows, err := d.ro.QueryContext(ctx,
		`SELECT id, project_id, branch, path, created_at, updated_at
		 FROM middleman_project_worktrees
		 WHERE project_id = ?
		 ORDER BY created_at, id`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list project worktrees: %w", err)
	}
	defer rows.Close()

	var worktrees []ProjectWorktree
	for rows.Next() {
		wt, err := scanProjectWorktreeRow(rows)
		if err != nil {
			return nil, err
		}
		worktrees = append(worktrees, *wt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project worktrees: %w", err)
	}
	return worktrees, nil
}

func scanProject(row *sql.Row) (*Project, error) {
	project, err := scanProjectFields(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan project: %w", err)
	}
	return project, nil
}

func scanProjectRow(rows *sql.Rows) (*Project, error) {
	project, err := scanProjectFields(rows)
	if err != nil {
		return nil, fmt.Errorf("scan project: %w", err)
	}
	return project, nil
}

func scanProjectFields(scanner interface{ Scan(...any) error }) (*Project, error) {
	var (
		p            Project
		defaultBr    sql.NullString
		platform     sql.NullString
		platformHost sql.NullString
		repoOwner    sql.NullString
		repoName     sql.NullString
	)
	err := scanner.Scan(
		&p.ID, &p.DisplayName, &p.LocalPath,
		&defaultBr, &p.CreatedAt, &p.UpdatedAt,
		&platform, &platformHost, &repoOwner, &repoName,
	)
	if err != nil {
		return nil, err
	}
	if defaultBr.Valid {
		p.DefaultBranch = defaultBr.String
	}
	if platformHost.Valid {
		p.PlatformIdentity = &PlatformIdentity{
			Platform: platform.String,
			Host:     platformHost.String,
			Owner:    repoOwner.String,
			Name:     repoName.String,
		}
	}
	return &p, nil
}

func scanProjectWorktree(row *sql.Row) (*ProjectWorktree, error) {
	worktree, err := scanProjectWorktreeFields(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan project worktree: %w", err)
	}
	return worktree, nil
}

func scanProjectWorktreeRow(rows *sql.Rows) (*ProjectWorktree, error) {
	worktree, err := scanProjectWorktreeFields(rows)
	if err != nil {
		return nil, fmt.Errorf("scan project worktree: %w", err)
	}
	return worktree, nil
}

func scanProjectWorktreeFields(
	scanner interface{ Scan(...any) error },
) (*ProjectWorktree, error) {
	var w ProjectWorktree
	if err := scanner.Scan(
		&w.ID, &w.ProjectID, &w.Branch, &w.Path,
		&w.CreatedAt, &w.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &w, nil
}

func newProjectID() (string, error) {
	return newPrefixedID("prj_")
}

func newWorktreeID() (string, error) {
	return newPrefixedID("wtr_")
}

func newPrefixedID(prefix string) (string, error) {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return prefix + hex.EncodeToString(buf[:]), nil
}

func isUniqueConstraintErr(err error, suffix string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") &&
		strings.Contains(msg, suffix)
}
