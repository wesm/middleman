package workspace

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/middleman/internal/db"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return d
}

func seedRepo(
	t *testing.T, d *db.DB,
	host, owner, name string,
) int64 {
	t.Helper()
	id, err := d.UpsertRepo(
		context.Background(), host, owner, name,
	)
	require.NoError(t, err)
	return id
}

func seedMR(
	t *testing.T, d *db.DB,
	repoID int64, number int, headBranch string,
) {
	t.Helper()
	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	mr := &db.MergeRequest{
		RepoID:         repoID,
		PlatformID:     repoID*10000 + int64(number),
		Number:         number,
		Title:          "Test PR",
		Author:         "author",
		State:          "open",
		HeadBranch:     headBranch,
		BaseBranch:     "main",
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	}
	_, err := d.UpsertMergeRequest(context.Background(), mr)
	require.NoError(t, err)
}

func seedMRWithFork(
	t *testing.T, d *db.DB,
	repoID int64, number int,
	headBranch, cloneURL string,
) {
	t.Helper()
	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	mr := &db.MergeRequest{
		RepoID:           repoID,
		PlatformID:       repoID*10000 + int64(number),
		Number:           number,
		Title:            "Fork PR",
		Author:           "contributor",
		State:            "open",
		HeadBranch:       headBranch,
		BaseBranch:       "main",
		HeadRepoCloneURL: cloneURL,
		CreatedAt:        now,
		UpdatedAt:        now,
		LastActivityAt:   now,
	}
	_, err := d.UpsertMergeRequest(context.Background(), mr)
	require.NoError(t, err)
}

func TestCreate(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	wtDir := t.TempDir()

	repoID := seedRepo(
		t, d, "github.com", "acme", "widget",
	)
	seedMR(t, d, repoID, 42, "feature/thing")

	mgr := NewManager(d, wtDir)

	ws, err := mgr.Create(
		ctx, "github.com", "acme", "widget", 42,
	)
	require.NoError(err)
	require.NotNil(ws)

	assert.NotEmpty(ws.ID)
	assert.Len(ws.ID, 16) // 8 bytes hex-encoded
	assert.Equal("creating", ws.Status)
	assert.Equal("github.com", ws.PlatformHost)
	assert.Equal("acme", ws.RepoOwner)
	assert.Equal("widget", ws.RepoName)
	assert.Equal(42, ws.MRNumber)
	assert.Equal("feature/thing", ws.MRHeadRef)
	assert.Nil(ws.MRHeadRepo)
	assert.Contains(ws.WorktreePath, "pr-42")
	assert.Equal("middleman-"+ws.ID, ws.TmuxSession)

	// Verify persisted in DB.
	got, err := d.GetWorkspace(ctx, ws.ID)
	require.NoError(err)
	require.NotNil(got)
	assert.Equal(ws.ID, got.ID)
	assert.Equal("creating", got.Status)
}

func TestCreateForkPR(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	wtDir := t.TempDir()

	repoID := seedRepo(
		t, d, "github.com", "acme", "widget",
	)
	seedMRWithFork(
		t, d, repoID, 99, "fix/typo",
		"https://github.com/contributor/widget.git",
	)

	mgr := NewManager(d, wtDir)

	ws, err := mgr.Create(
		ctx, "github.com", "acme", "widget", 99,
	)
	require.NoError(t, err)
	require.NotNil(t, ws)

	assert.NotNil(ws.MRHeadRepo)
	assert.Equal(
		"https://github.com/contributor/widget.git",
		*ws.MRHeadRepo,
	)
}

func TestCreateRepoNotTracked(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	mgr := NewManager(d, t.TempDir())

	_, err := mgr.Create(
		ctx, "github.com", "unknown", "repo", 1,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "repository not tracked")
}

func TestCreateDuplicate(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	wtDir := t.TempDir()

	repoID := seedRepo(
		t, d, "github.com", "acme", "widget",
	)
	seedMR(t, d, repoID, 42, "feature/thing")

	mgr := NewManager(d, wtDir)

	// First create succeeds.
	ws, err := mgr.Create(
		ctx, "github.com", "acme", "widget", 42,
	)
	require.NoError(err)
	require.NotNil(ws)

	// Second create for same MR fails with unique constraint.
	_, err = mgr.Create(
		ctx, "github.com", "acme", "widget", 42,
	)
	require.Error(err)
	require.Contains(err.Error(), "UNIQUE constraint")
}

func TestCreateMRNotSynced(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedRepo(t, d, "github.com", "acme", "widget")

	mgr := NewManager(d, t.TempDir())

	_, err := mgr.Create(
		ctx, "github.com", "acme", "widget", 999,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not synced yet")
}

func TestShellFromPasswdLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			"normal zsh",
			"wesm:x:501:20:Wes McKinney:/Users/wesm:/bin/zsh",
			"/bin/zsh",
		},
		{
			"normal bash",
			"dev:x:1000:1000::/home/dev:/bin/bash",
			"/bin/bash",
		},
		{
			"nologin filtered",
			"nobody:x:65534:65534:Nobody:/nonexistent:/sbin/nologin",
			"",
		},
		{
			"false filtered",
			"git:x:998:998::/home/git:/usr/bin/false",
			"",
		},
		{
			"bin/false filtered",
			"svc:x:999:999::/srv:/bin/false",
			"",
		},
		{
			"empty shell",
			"user:x:1000:1000::/home/user:",
			"",
		},
		{
			"too few fields",
			"broken:line",
			"",
		},
		{
			"empty line",
			"",
			"",
		},
		{
			"trailing whitespace",
			"user:x:1000:1000::/home/user:/bin/zsh\n",
			"/bin/zsh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellFromPasswdLine(tt.line)
			require.Equal(t, tt.want, got)
		})
	}
}
