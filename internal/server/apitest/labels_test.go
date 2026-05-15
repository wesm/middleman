package apitest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/db"
)

type labelAPIResponse struct {
	Labels []db.Label `json:"labels"`
}

func doLabelAPIRequest(
	t *testing.T,
	srv http.Handler,
	method string,
	path string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	var payload bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&payload).Encode(body))
	}
	req := httptest.NewRequest(method, path, &payload)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	return rr
}

func seedRepoLabelCatalog(
	t *testing.T,
	database *db.DB,
	owner string,
	name string,
) int64 {
	t.Helper()
	ctx := t.Context()
	repo, err := database.GetRepoByOwnerName(ctx, owner, name)
	require.NoError(t, err)
	require.NotNil(t, repo)
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	require.NoError(t, database.ReplaceRepoLabelCatalog(ctx, repo.ID, []db.Label{
		{Name: "bug", Description: "Something is broken", Color: "d73a4a", IsDefault: true, UpdatedAt: now},
		{Name: "triage", Description: "Needs review", Color: "fbca04", UpdatedAt: now},
	}, now))
	return repo.ID
}

func TestAPIListRepoLabelsReturnsCachedCatalog(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	seedRepoLabelCatalog(t, database, "acme", "widget")

	rr := doLabelAPIRequest(t, srv, http.MethodGet, "/api/v1/repo/github/acme/widget/labels", nil)
	require.Equal(http.StatusOK, rr.Code)

	var body labelAPIResponse
	require.NoError(json.Unmarshal(rr.Body.Bytes(), &body))
	require.Len(body.Labels, 2)
	assert.Equal("bug", body.Labels[0].Name)
	assert.Equal("triage", body.Labels[1].Name)
}

func TestAPISetPullLabelsReplacesAssignedLabelsFromCatalog(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPRWithLabels(t, database, "acme", "widget", 1, []db.Label{{Name: "bug", Color: "d73a4a"}})
	repoID := seedRepoLabelCatalog(t, database, "acme", "widget")

	rr := doLabelAPIRequest(t, srv, http.MethodPut, "/api/v1/pulls/github/acme/widget/1/labels", map[string][]string{
		"labels": {"triage"},
	})
	require.Equal(http.StatusOK, rr.Code)

	var body labelAPIResponse
	require.NoError(json.Unmarshal(rr.Body.Bytes(), &body))
	require.Len(body.Labels, 1)
	require.Equal("triage", body.Labels[0].Name)

	pr, err := database.GetMergeRequestByRepoIDAndNumber(t.Context(), repoID, 1)
	require.NoError(err)
	require.NotNil(pr)
	require.Len(pr.Labels, 1)
	require.Equal("triage", pr.Labels[0].Name)
}

func TestAPISetIssueLabelsRejectsLabelsOutsideCatalog(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedIssue(t, database, "acme", "widget", 7, "open")
	seedRepoLabelCatalog(t, database, "acme", "widget")

	rr := doLabelAPIRequest(t, srv, http.MethodPut, "/api/v1/issues/github/acme/widget/7/labels", map[string][]string{
		"labels": {"not-in-catalog"},
	})
	require.Equal(http.StatusBadRequest, rr.Code)
}
