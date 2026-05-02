package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	dbsqlc "github.com/wesm/middleman/internal/db/sqlc"
)

// ListPRsForStackDetection returns non-closed PRs for a repo (open + merged).
func (d *DB) ListPRsForStackDetection(ctx context.Context, repoID int64) ([]MergeRequest, error) {
	rows, err := d.readQueries.ListPRsForStackDetection(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("list prs for stack detection: %w", err)
	}

	prs := make([]MergeRequest, 0, len(rows))
	for _, row := range rows {
		prs = append(prs, MergeRequest{
			ID:             row.ID,
			RepoID:         repoID,
			Number:         int(row.Number),
			Title:          row.Title,
			HeadBranch:     row.HeadBranch,
			BaseBranch:     row.BaseBranch,
			State:          row.State,
			CIStatus:       row.CiStatus,
			ReviewDecision: row.ReviewDecision,
		})
	}
	return prs, nil
}

func stackWithRepoFromSQL(
	id, repoID, baseNumber int64,
	name string,
	createdAt, updatedAt time.Time,
	owner, repoName string,
) StackWithRepo {
	return StackWithRepo{
		Stack: Stack{
			ID:         id,
			RepoID:     repoID,
			BaseNumber: int(baseNumber),
			Name:       name,
			CreatedAt:  createdAt,
			UpdatedAt:  updatedAt,
		},
		RepoOwner: owner,
		RepoName:  repoName,
	}
}

func stackMemberWithPRFromSQL(
	stackID, mergeRequestID, position, number int64,
	title, state, ciStatus, reviewDecision string,
	isDraft int64,
	baseBranch string,
) StackMemberWithPR {
	return StackMemberWithPR{
		StackID:        stackID,
		MergeRequestID: mergeRequestID,
		Position:       int(position),
		Number:         int(number),
		Title:          title,
		State:          state,
		CIStatus:       ciStatus,
		ReviewDecision: reviewDecision,
		IsDraft:        isDraft != 0,
		BaseBranch:     baseBranch,
	}
}

func stackFromSQL(row dbsqlc.MiddlemanStack) Stack {
	return Stack{
		ID:         row.ID,
		RepoID:     row.RepoID,
		BaseNumber: int(row.BaseNumber),
		Name:       row.Name,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}
}

func listStackMembersByStackIDs(
	ctx context.Context,
	q *dbsqlc.Queries,
	stackIDs []int64,
) (map[int64][]StackMemberWithPR, error) {
	rows, err := q.ListStackMembersByStackIDs(ctx, stackIDs)
	if err != nil {
		return nil, fmt.Errorf("list stack members: %w", err)
	}
	memberMap := make(map[int64][]StackMemberWithPR)
	for _, row := range rows {
		member := stackMemberWithPRFromSQL(
			row.StackID,
			row.MergeRequestID,
			row.Position,
			row.Number,
			row.Title,
			row.State,
			row.CiStatus,
			row.ReviewDecision,
			row.IsDraft,
			row.BaseBranch,
		)
		memberMap[row.StackID] = append(memberMap[row.StackID], member)
	}
	return memberMap, nil
}

func listStackMembersByStack(
	ctx context.Context,
	q *dbsqlc.Queries,
	stackID int64,
) ([]StackMemberWithPR, error) {
	rows, err := q.ListStackMembersByStack(ctx, stackID)
	if err != nil {
		return nil, fmt.Errorf("get stack members: %w", err)
	}
	members := make([]StackMemberWithPR, 0, len(rows))
	for _, row := range rows {
		members = append(members, stackMemberWithPRFromSQL(
			row.StackID,
			row.MergeRequestID,
			row.Position,
			row.Number,
			row.Title,
			row.State,
			row.CiStatus,
			row.ReviewDecision,
			row.IsDraft,
			row.BaseBranch,
		))
	}
	return members, nil
}

// UpsertStack inserts or updates a stack keyed by (repo_id, base_number).
func (d *DB) UpsertStack(ctx context.Context, repoID int64, baseNumber int, name string) (int64, error) {
	id, err := d.writeQueries.UpsertStack(ctx, dbsqlc.UpsertStackParams{
		RepoID:     repoID,
		BaseNumber: int64(baseNumber),
		Name:       name,
	})
	if err != nil {
		return 0, fmt.Errorf("upsert stack: %w", err)
	}
	return id, nil
}

// ReplaceStackMembers atomically replaces all members of a stack.
// Also removes the new members from any other stack they might belong to,
// so PRs can be reassigned between stacks without violating the unique
// merge_request_id constraint.
func (d *DB) ReplaceStackMembers(ctx context.Context, stackID int64, members []StackMember) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		q := dbsqlc.New(tx)
		if err := q.DeleteStackMembersByStack(ctx, stackID); err != nil {
			return fmt.Errorf("delete old stack members: %w", err)
		}
		if len(members) == 0 {
			return nil
		}
		// Evict these PRs from any other stack to avoid unique-index conflict.
		for _, m := range members {
			if err := q.DeleteStackMemberByMR(ctx, m.MergeRequestID); err != nil {
				return fmt.Errorf("evict existing stack member: %w", err)
			}
		}
		for _, m := range members {
			if err := q.InsertStackMember(ctx, dbsqlc.InsertStackMemberParams{
				StackID:        stackID,
				MergeRequestID: m.MergeRequestID,
				Position:       int64(m.Position),
			}); err != nil {
				return fmt.Errorf("insert stack member: %w", err)
			}
		}
		return nil
	})
}

// ListStacksWithMembers returns stacks with repo info and their members.
// Only stacks that have at least one open member are returned.
func (d *DB) ListStacksWithMembers(ctx context.Context, repoFilter string) ([]StackWithRepo, map[int64][]StackMemberWithPR, error) {
	var (
		stacks   []StackWithRepo
		stackIDs []int64
	)
	if repoFilter != "" {
		if strings.Count(repoFilter, "/") != 1 {
			return nil, nil, fmt.Errorf("invalid repo filter %q: expected owner/name", repoFilter)
		}
		owner, name, _ := strings.Cut(repoFilter, "/")
		if owner == "" || name == "" {
			return nil, nil, fmt.Errorf("invalid repo filter %q: expected owner/name", repoFilter)
		}
		_, owner, name = canonicalRepoIdentifier("", owner, name)
		rows, err := d.readQueries.ListStacksWithOpenMembersByRepo(
			ctx,
			dbsqlc.ListStacksWithOpenMembersByRepoParams{
				Owner: owner,
				Name:  name,
			},
		)
		if err != nil {
			return nil, nil, fmt.Errorf("list stacks: %w", err)
		}
		stacks = make([]StackWithRepo, 0, len(rows))
		stackIDs = make([]int64, 0, len(rows))
		for _, row := range rows {
			stack := stackWithRepoFromSQL(
				row.ID,
				row.RepoID,
				row.BaseNumber,
				row.Name,
				row.CreatedAt,
				row.UpdatedAt,
				row.Owner,
				row.RepoName,
			)
			stacks = append(stacks, stack)
			stackIDs = append(stackIDs, stack.ID)
		}
	} else {
		rows, err := d.readQueries.ListStacksWithOpenMembers(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("list stacks: %w", err)
		}
		stacks = make([]StackWithRepo, 0, len(rows))
		stackIDs = make([]int64, 0, len(rows))
		for _, row := range rows {
			stack := stackWithRepoFromSQL(
				row.ID,
				row.RepoID,
				row.BaseNumber,
				row.Name,
				row.CreatedAt,
				row.UpdatedAt,
				row.Owner,
				row.RepoName,
			)
			stacks = append(stacks, stack)
			stackIDs = append(stackIDs, stack.ID)
		}
	}
	if len(stackIDs) == 0 {
		return stacks, make(map[int64][]StackMemberWithPR), nil
	}

	memberMap, err := listStackMembersByStackIDs(ctx, d.readQueries, stackIDs)
	if err != nil {
		return nil, nil, err
	}
	return stacks, memberMap, nil
}

// DeleteStaleStacks removes stacks for a repo that are not in the active set.
func (d *DB) DeleteStaleStacks(ctx context.Context, repoID int64, activeStackIDs []int64) error {
	if len(activeStackIDs) == 0 {
		if err := d.writeQueries.DeleteAllStacksForRepo(ctx, repoID); err != nil {
			return fmt.Errorf("delete all stacks for repo: %w", err)
		}
		return nil
	}
	if err := d.writeQueries.DeleteStaleStacksForRepo(ctx, dbsqlc.DeleteStaleStacksForRepoParams{
		RepoID:         repoID,
		ActiveStackIds: activeStackIDs,
	}); err != nil {
		return fmt.Errorf("delete stale stacks: %w", err)
	}
	return nil
}

// GetStackForPR returns the stack and members for a specific PR, or nil if not in a stack.
func (d *DB) GetStackForPR(ctx context.Context, owner, name string, number int) (*Stack, []StackMemberWithPR, error) {
	_, owner, name = canonicalRepoIdentifier("", owner, name)
	row, err := d.readQueries.GetStackForPR(ctx, dbsqlc.GetStackForPRParams{
		Owner:  owner,
		Name:   name,
		Number: int64(number),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get stack for pr: %w", err)
	}
	stack := stackFromSQL(row)

	members, err := listStackMembersByStack(ctx, d.readQueries, stack.ID)
	if err != nil {
		return nil, nil, err
	}
	return &stack, members, nil
}
