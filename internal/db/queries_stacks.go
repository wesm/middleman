package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ListPRsForStackDetection returns non-closed PRs for a repo (open + merged).
func (d *DB) ListPRsForStackDetection(ctx context.Context, repoID int64) ([]MergeRequest, error) {
	rows, err := d.ro.QueryContext(ctx, `
		SELECT id, number, title, head_branch, base_branch, state, ci_status, review_decision
		FROM middleman_merge_requests
		WHERE repo_id = ? AND state IN ('open', 'merged')
		ORDER BY number`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("list prs for stack detection: %w", err)
	}
	defer rows.Close()

	var prs []MergeRequest
	for rows.Next() {
		var mr MergeRequest
		mr.RepoID = repoID
		if err := rows.Scan(
			&mr.ID, &mr.Number, &mr.Title, &mr.HeadBranch, &mr.BaseBranch,
			&mr.State, &mr.CIStatus, &mr.ReviewDecision,
		); err != nil {
			return nil, fmt.Errorf("scan pr for stack detection: %w", err)
		}
		prs = append(prs, mr)
	}
	return prs, rows.Err()
}

// UpsertStack inserts or updates a stack keyed by (repo_id, base_number).
func (d *DB) UpsertStack(ctx context.Context, repoID int64, baseNumber int, name string) (int64, error) {
	res, err := d.rw.ExecContext(ctx, `
		INSERT INTO middleman_stacks (repo_id, base_number, name)
		VALUES (?, ?, ?)
		ON CONFLICT(repo_id, base_number) DO UPDATE SET
			name = excluded.name, updated_at = datetime('now')`,
		repoID, baseNumber, name,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert stack: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		err = d.ro.QueryRowContext(ctx,
			`SELECT id FROM middleman_stacks WHERE repo_id = ? AND base_number = ?`,
			repoID, baseNumber,
		).Scan(&id)
		if err != nil {
			return 0, fmt.Errorf("get stack id: %w", err)
		}
	}
	return id, nil
}

// ReplaceStackMembers atomically replaces all members of a stack.
func (d *DB) ReplaceStackMembers(ctx context.Context, stackID int64, members []StackMember) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM middleman_stack_members WHERE stack_id = ?`, stackID,
		); err != nil {
			return fmt.Errorf("delete old stack members: %w", err)
		}
		if len(members) == 0 {
			return nil
		}
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO middleman_stack_members (stack_id, merge_request_id, position)
			VALUES (?, ?, ?)`)
		if err != nil {
			return fmt.Errorf("prepare insert stack member: %w", err)
		}
		defer stmt.Close()
		for _, m := range members {
			if _, err := stmt.ExecContext(ctx, stackID, m.MergeRequestID, m.Position); err != nil {
				return fmt.Errorf("insert stack member: %w", err)
			}
		}
		return nil
	})
}

// ListStacksWithMembers returns stacks with repo info and their members.
// Only stacks that have at least one open member are returned.
func (d *DB) ListStacksWithMembers(ctx context.Context, repoFilter string) ([]StackWithRepo, map[int64][]StackMemberWithPR, error) {
	var conds []string
	var args []any
	if repoFilter != "" {
		if strings.Count(repoFilter, "/") != 1 {
			return nil, nil, fmt.Errorf("invalid repo filter %q: expected owner/name", repoFilter)
		}
		owner, name, _ := strings.Cut(repoFilter, "/")
		if owner == "" || name == "" {
			return nil, nil, fmt.Errorf("invalid repo filter %q: expected owner/name", repoFilter)
		}
		conds = append(conds, "r.owner = ? AND r.name = ?")
		args = append(args, owner, name)
	}
	conds = append(conds, `EXISTS (
		SELECT 1 FROM middleman_stack_members sm2
		JOIN middleman_merge_requests p2 ON p2.id = sm2.merge_request_id
		WHERE sm2.stack_id = s.id AND p2.state = 'open')`)

	where := "WHERE " + strings.Join(conds, " AND ")

	stackQuery := fmt.Sprintf(`
		SELECT s.id, s.repo_id, s.base_number, s.name, s.created_at, s.updated_at,
		       r.owner, r.name
		FROM middleman_stacks s
		JOIN middleman_repos r ON r.id = s.repo_id
		%s
		ORDER BY s.updated_at DESC`, where)

	rows, err := d.ro.QueryContext(ctx, stackQuery, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("list stacks: %w", err)
	}
	defer rows.Close()

	var stacks []StackWithRepo
	var stackIDs []int64
	for rows.Next() {
		var s StackWithRepo
		if err := rows.Scan(
			&s.ID, &s.RepoID, &s.BaseNumber, &s.Name, &s.CreatedAt, &s.UpdatedAt,
			&s.RepoOwner, &s.RepoName,
		); err != nil {
			return nil, nil, fmt.Errorf("scan stack: %w", err)
		}
		stacks = append(stacks, s)
		stackIDs = append(stackIDs, s.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if len(stackIDs) == 0 {
		return stacks, make(map[int64][]StackMemberWithPR), nil
	}

	// Fetch members for all stacks.
	placeholders := make([]string, len(stackIDs))
	memberArgs := make([]any, len(stackIDs))
	for i, id := range stackIDs {
		placeholders[i] = "?"
		memberArgs[i] = id
	}
	memberQuery := `
		SELECT sm.stack_id, sm.merge_request_id, sm.position,
		       p.number, p.title, p.state, p.ci_status, p.review_decision, p.is_draft
		FROM middleman_stack_members sm
		JOIN middleman_merge_requests p ON p.id = sm.merge_request_id
		WHERE sm.stack_id IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY sm.stack_id, sm.position`

	mRows, err := d.ro.QueryContext(ctx, memberQuery, memberArgs...)
	if err != nil {
		return nil, nil, fmt.Errorf("list stack members: %w", err)
	}
	defer mRows.Close()

	memberMap := make(map[int64][]StackMemberWithPR)
	for mRows.Next() {
		var m StackMemberWithPR
		if err := mRows.Scan(
			&m.StackID, &m.MergeRequestID, &m.Position,
			&m.Number, &m.Title, &m.State, &m.CIStatus, &m.ReviewDecision, &m.IsDraft,
		); err != nil {
			return nil, nil, fmt.Errorf("scan stack member: %w", err)
		}
		memberMap[m.StackID] = append(memberMap[m.StackID], m)
	}
	return stacks, memberMap, mRows.Err()
}

// DeleteStaleStacks removes stacks for a repo that are not in the active set.
func (d *DB) DeleteStaleStacks(ctx context.Context, repoID int64, activeStackIDs []int64) error {
	if len(activeStackIDs) == 0 {
		_, err := d.rw.ExecContext(ctx,
			`DELETE FROM middleman_stacks WHERE repo_id = ?`, repoID)
		if err != nil {
			return fmt.Errorf("delete all stacks for repo: %w", err)
		}
		return nil
	}
	placeholders := make([]string, len(activeStackIDs))
	args := make([]any, 0, len(activeStackIDs)+1)
	args = append(args, repoID)
	for i, id := range activeStackIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	_, err := d.rw.ExecContext(ctx,
		`DELETE FROM middleman_stacks WHERE repo_id = ? AND id NOT IN (`+
			strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return fmt.Errorf("delete stale stacks: %w", err)
	}
	return nil
}

// GetStackForPR returns the stack and members for a specific PR, or nil if not in a stack.
func (d *DB) GetStackForPR(ctx context.Context, owner, name string, number int) (*Stack, []StackMemberWithPR, error) {
	var stack Stack
	err := d.ro.QueryRowContext(ctx, `
		SELECT s.id, s.repo_id, s.base_number, s.name, s.created_at, s.updated_at
		FROM middleman_stacks s
		JOIN middleman_stack_members sm ON sm.stack_id = s.id
		JOIN middleman_merge_requests p ON p.id = sm.merge_request_id
		JOIN middleman_repos r ON r.id = p.repo_id
		WHERE r.owner = ? AND r.name = ? AND p.number = ?`,
		owner, name, number,
	).Scan(&stack.ID, &stack.RepoID, &stack.BaseNumber, &stack.Name, &stack.CreatedAt, &stack.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get stack for pr: %w", err)
	}

	rows, err := d.ro.QueryContext(ctx, `
		SELECT sm.stack_id, sm.merge_request_id, sm.position,
		       p.number, p.title, p.state, p.ci_status, p.review_decision, p.is_draft
		FROM middleman_stack_members sm
		JOIN middleman_merge_requests p ON p.id = sm.merge_request_id
		WHERE sm.stack_id = ?
		ORDER BY sm.position`, stack.ID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("get stack members: %w", err)
	}
	defer rows.Close()

	var members []StackMemberWithPR
	for rows.Next() {
		var m StackMemberWithPR
		if err := rows.Scan(
			&m.StackID, &m.MergeRequestID, &m.Position,
			&m.Number, &m.Title, &m.State, &m.CIStatus, &m.ReviewDecision, &m.IsDraft,
		); err != nil {
			return nil, nil, fmt.Errorf("scan stack member: %w", err)
		}
		members = append(members, m)
	}
	return &stack, members, rows.Err()
}
