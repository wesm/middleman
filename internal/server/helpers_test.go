package server

import (
	"context"
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
