package terminal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/workspace"
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

func TestHandlerWorkspaceNotFound(t *testing.T) {
	d := openTestDB(t)
	mgr := workspace.NewManager(d, t.TempDir())
	h := &Handler{Workspaces: mgr}

	req := httptest.NewRequest(
		http.MethodGet, "/api/v1/workspaces/nonexistent/terminal",
		nil,
	)
	req.SetPathValue("id", "nonexistent")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert := Assert.New(t)
	assert.Equal(http.StatusNotFound, rec.Code)
	assert.Contains(rec.Body.String(), "not found")
}

func TestHandlerWorkspaceNotReady(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	wtDir := t.TempDir()

	repoID := seedRepo(t, d, "github.com", "acme", "widget")
	seedMR(t, d, repoID, 42, "feature/thing")

	mgr := workspace.NewManager(d, wtDir)
	ws, err := mgr.Create(
		ctx, "github.com", "acme", "widget", 42,
	)
	require.NoError(t, err)
	require.Equal(t, "creating", ws.Status)

	h := &Handler{Workspaces: mgr}

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/workspaces/"+ws.ID+"/terminal",
		nil,
	)
	req.SetPathValue("id", ws.ID)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert := Assert.New(t)
	assert.Equal(http.StatusConflict, rec.Code)
	assert.Contains(rec.Body.String(), "not ready")
}
