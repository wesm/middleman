package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/db"
)

func TestParseRepoNumberPathParsesRouteValues(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/repos/acme/widget/pulls/42", nil)
	req.SetPathValue("owner", "acme")
	req.SetPathValue("name", "widget")
	req.SetPathValue("number", "42")
	rr := httptest.NewRecorder()

	ref, ok := parseRepoNumberPath(rr, req, "pull request")
	require.True(t, ok)
	require.Equal(t, "acme", ref.owner)
	require.Equal(t, "widget", ref.name)
	require.Equal(t, 42, ref.number)
}

func TestParseRepoNumberPathRejectsInvalidNumber(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/repos/acme/widget/pulls/nope", nil)
	req.SetPathValue("owner", "acme")
	req.SetPathValue("name", "widget")
	req.SetPathValue("number", "nope")
	rr := httptest.NewRecorder()

	_, ok := parseRepoNumberPath(rr, req, "pull request")
	require.False(t, ok)
	require.Equal(t, 400, rr.Code)
}

func TestBuildRepoLookupMapsIDs(t *testing.T) {
	lookup := buildRepoLookup([]db.Repo{
		{ID: 1, Owner: "acme", Name: "widget"},
		{ID: 2, Owner: "octo", Name: "thing"},
	})

	require.Equal(t, db.Repo{ID: 1, Owner: "acme", Name: "widget"}, lookup[1])
	require.Equal(t, db.Repo{ID: 2, Owner: "octo", Name: "thing"}, lookup[2])
}

func TestLookupRepoIDReturnsExistingRepoID(t *testing.T) {
	srv, database := setupTestServer(t)
	ctx := context.Background()

	repoID, err := database.UpsertRepo(ctx, "acme", "widget")
	require.NoError(t, err)

	got, err := srv.lookupRepoID(ctx, "acme", "widget")
	require.NoError(t, err)
	require.Equal(t, repoID, got)
}

func TestParseStarredRequestParsesValidBodyAndRepoID(t *testing.T) {
	srv, database := setupTestServer(t)
	_, err := database.UpsertRepo(context.Background(), "acme", "widget")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/starred", bytes.NewBufferString(`{"item_type":"pr","owner":"acme","name":"widget","number":1}`))
	rr := httptest.NewRecorder()

	body, repoID, ok := srv.parseStarredRequest(rr, req)
	require.True(t, ok)
	require.Equal(t, "pr", body.ItemType)
	require.Equal(t, "acme", body.Owner)
	require.Equal(t, "widget", body.Name)
	require.Equal(t, 1, body.Number)
	require.NotZero(t, repoID)
}

func TestParseStarredRequestRejectsInvalidItemType(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/starred", bytes.NewBufferString(`{"item_type":"repo","owner":"acme","name":"widget","number":1}`))
	rr := httptest.NewRecorder()

	_, _, ok := srv.parseStarredRequest(rr, req)
	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.Contains(t, rr.Body.String(), "item_type must be 'pr' or 'issue'")
}

func TestParseStarredRequestRejectsMissingRepo(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/starred", bytes.NewBufferString(`{"item_type":"pr","owner":"acme","name":"widget","number":1}`))
	rr := httptest.NewRecorder()

	_, _, ok := srv.parseStarredRequest(rr, req)
	require.False(t, ok)
	require.Equal(t, http.StatusNotFound, rr.Code)
	require.Contains(t, rr.Body.String(), "repo not found")
}
