package db

import (
	"context"
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertTestMRWithBranches(t *testing.T, d *DB, repoID int64, number int, head, base, state string) int64 {
	t.Helper()
	now := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	mr := &MergeRequest{
		RepoID:         repoID,
		PlatformID:     repoID*10000 + int64(number),
		Number:         number,
		Title:          "PR " + head,
		Author:         "author",
		State:          state,
		HeadBranch:     head,
		BaseBranch:     base,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	}
	id, err := d.UpsertMergeRequest(context.Background(), mr)
	require.NoError(t, err)
	return id
}

func TestListPRsForStackDetection(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	repoID := insertTestRepo(t, d, "org", "repo")

	// open PR — included
	insertTestMRWithBranches(t, d, repoID, 1, "feature/a", "main", "open")
	// merged PR — included
	insertTestMRWithBranches(t, d, repoID, 2, "feature/b", "feature/a", "merged")
	// closed PR — excluded
	insertTestMRWithBranches(t, d, repoID, 3, "feature/c", "main", "closed")

	prs, err := d.ListPRsForStackDetection(ctx, repoID)
	require.NoError(t, err)
	assert.Len(prs, 2)
	numbers := []int{prs[0].Number, prs[1].Number}
	assert.ElementsMatch([]int{1, 2}, numbers)
}

func TestUpsertStackAndReplaceMembers(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	repoID := insertTestRepo(t, d, "org", "repo")

	mrID1 := insertTestMRWithBranches(t, d, repoID, 1, "feature/a", "main", "open")
	mrID2 := insertTestMRWithBranches(t, d, repoID, 2, "feature/b", "feature/a", "open")

	// Create stack (keyed by repo_id + base_number)
	stackID, err := d.UpsertStack(ctx, repoID, 1, "feature")
	require.NoError(t, err)
	assert.Positive(stackID)

	// Idempotent upsert returns same ID
	stackID2, err := d.UpsertStack(ctx, repoID, 1, "feature")
	require.NoError(t, err)
	assert.Equal(stackID, stackID2)

	// Replace members
	members := []StackMember{
		{StackID: stackID, MergeRequestID: mrID1, Position: 1},
		{StackID: stackID, MergeRequestID: mrID2, Position: 2},
	}
	err = d.ReplaceStackMembers(ctx, stackID, members)
	require.NoError(t, err)

	// Verify via ListStacksWithMembers
	stacks, memberMap, err := d.ListStacksWithMembers(ctx, "")
	require.NoError(t, err)
	assert.Len(stacks, 1)
	assert.Equal("feature", stacks[0].Name)
	assert.Equal("org", stacks[0].RepoOwner)
	assert.Equal("repo", stacks[0].RepoName)
	assert.Len(memberMap[stackID], 2)
	assert.Equal(1, memberMap[stackID][0].Position)
	assert.Equal(2, memberMap[stackID][1].Position)
}

func TestDeleteStaleStacks(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	repoID := insertTestRepo(t, d, "org", "repo")

	id1, err := d.UpsertStack(ctx, repoID, 1, "keep")
	require.NoError(t, err)
	_, err = d.UpsertStack(ctx, repoID, 2, "delete-me")
	require.NoError(t, err)

	err = d.DeleteStaleStacks(ctx, repoID, []int64{id1})
	require.NoError(t, err)

	// Verify directly that "delete-me" is gone.
	var count int
	err = d.ReadDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM middleman_stacks WHERE repo_id = ?`, repoID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(1, count) // only "keep" remains
}

func TestListStacksWithMembers_MalformedFilter(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	for _, bad := range []string{"noslash", "/bar", "foo/", "/", "foo/bar/baz"} {
		_, _, err := d.ListStacksWithMembers(ctx, bad)
		require.Error(t, err, "filter=%q should fail", bad)
	}
	// Empty string is valid (no filter).
	_, _, err := d.ListStacksWithMembers(ctx, "")
	require.NoError(t, err)
}

func TestUniqueStackMemberConstraint(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	repoID := insertTestRepo(t, d, "org", "repo")

	mrID := insertTestMRWithBranches(t, d, repoID, 1, "feature/a", "main", "open")

	stackA, err := d.UpsertStack(ctx, repoID, 1, "stackA")
	require.NoError(t, err)
	err = d.ReplaceStackMembers(ctx, stackA, []StackMember{
		{StackID: stackA, MergeRequestID: mrID, Position: 1},
	})
	require.NoError(t, err)

	// Same PR in a different stack should fail unique constraint.
	stackB, err := d.UpsertStack(ctx, repoID, 2, "stackB")
	require.NoError(t, err)
	err = d.ReplaceStackMembers(ctx, stackB, []StackMember{
		{StackID: stackB, MergeRequestID: mrID, Position: 1},
	})
	require.Error(t, err)
}

func TestGetStackForPR(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	repoID := insertTestRepo(t, d, "org", "repo")

	mrID1 := insertTestMRWithBranches(t, d, repoID, 10, "feature/a", "main", "open")
	mrID2 := insertTestMRWithBranches(t, d, repoID, 11, "feature/b", "feature/a", "open")

	stackID, err := d.UpsertStack(ctx, repoID, 10, "feature")
	require.NoError(t, err)
	err = d.ReplaceStackMembers(ctx, stackID, []StackMember{
		{StackID: stackID, MergeRequestID: mrID1, Position: 1},
		{StackID: stackID, MergeRequestID: mrID2, Position: 2},
	})
	require.NoError(t, err)

	// Found
	stack, members, err := d.GetStackForPR(ctx, "org", "repo", 10)
	require.NoError(t, err)
	require.NotNil(t, stack)
	assert.Equal("feature", stack.Name)
	assert.Len(members, 2)

	// Not found
	stack2, _, err := d.GetStackForPR(ctx, "org", "repo", 999)
	require.NoError(t, err)
	assert.Nil(stack2)
}
