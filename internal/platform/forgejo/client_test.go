package forgejo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	Require "github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/platform"
	"github.com/wesm/middleman/internal/platform/gitealike"
)

var (
	_ gitealike.Transport         = (*transport)(nil)
	_ gitealike.ActionsTransport  = (*transport)(nil)
	_ platform.RepositoryReader   = (*Client)(nil)
	_ platform.MergeRequestReader = (*Client)(nil)
	_ platform.IssueReader        = (*Client)(nil)
	_ platform.ReleaseReader      = (*Client)(nil)
	_ platform.TagReader          = (*Client)(nil)
	_ platform.CIReader           = (*Client)(nil)
)

func TestClientLooksUpRepositoryAndSendsToken(t *testing.T) {
	assert := Assert.New(t)
	require := Require.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(http.MethodGet, r.Method)
		assert.Equal("/api/v1/repos/owner/repo", r.URL.Path)
		assert.Equal("token forgejo-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(json.NewEncoder(w).Encode(map[string]any{
			"id":        1,
			"name":      "repo",
			"full_name": "owner/repo",
			"owner": map[string]any{
				"id":         2,
				"login":      "owner",
				"full_name":  "Owner",
				"avatar_url": "",
				"html_url":   "",
			},
		}))
	}))
	defer server.Close()

	client, err := NewClient(
		"codeberg.test",
		"forgejo-token",
		WithBaseURLForTesting(server.URL),
	)
	require.NoError(err)

	repo, err := client.transport.getRepositoryRaw(context.Background(), "owner", "repo")
	require.NoError(err)
	assert.Equal("repo", repo.Name)
}

func TestClientLookupUsesForegroundTimeout(t *testing.T) {
	require := Require.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(
		"codeberg.test",
		"forgejo-token",
		WithBaseURLForTesting(server.URL),
		WithForegroundTimeoutForTesting(20*time.Millisecond),
	)
	require.NoError(err)

	_, err = client.transport.getRepositoryRaw(context.Background(), "owner", "repo")
	require.Error(err)
}

func TestClientProviderIdentityExposesReadCapabilities(t *testing.T) {
	assert := Assert.New(t)
	require := Require.New(t)
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	client, err := NewClient(
		"codeberg.test",
		"forgejo-token",
		WithBaseURLForTesting(server.URL),
	)
	require.NoError(err)

	assert.Equal(platform.KindForgejo, client.Platform())
	assert.Equal("codeberg.test", client.Host())
	assert.Equal(platform.Capabilities{
		ReadRepositories:  true,
		ReadMergeRequests: true,
		ReadIssues:        true,
		ReadComments:      true,
		ReadReleases:      true,
		ReadCI:            true,
	}, client.Capabilities())
}

func TestClientReadsOpenPullRequestsIssuesAndCIChecks(t *testing.T) {
	assert := Assert.New(t)
	require := Require.New(t)

	var sawPulls, sawIssues, sawStatuses, sawActions bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal("token forgejo-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/repos/owner/repo/pulls":
			sawPulls = true
			assert.Equal("open", r.URL.Query().Get("state"))
			assert.Equal("1", r.URL.Query().Get("page"))
			assert.Equal("100", r.URL.Query().Get("limit"))
			assert.NoError(json.NewEncoder(w).Encode([]map[string]any{{
				"id": 11, "number": 3, "url": "https://codeberg.test/owner/repo/pulls/3",
				"title": "review me", "state": "open", "user": map[string]any{"login": "alice"},
				"head": map[string]any{"ref": "feature", "sha": "abc"},
				"base": map[string]any{"ref": "main", "sha": "def"},
			}}))
		case "/api/v1/repos/owner/repo/issues":
			sawIssues = true
			assert.Equal("open", r.URL.Query().Get("state"))
			assert.NoError(json.NewEncoder(w).Encode([]map[string]any{{
				"id": 21, "number": 4, "url": "https://codeberg.test/owner/repo/issues/4",
				"title": "bug", "state": "open", "user": map[string]any{"login": "bob"},
			}}))
		case "/api/v1/repos/owner/repo/commits/abc/statuses":
			sawStatuses = true
			assert.NoError(json.NewEncoder(w).Encode([]map[string]any{{
				"id": 31, "context": "build", "state": "success", "target_url": "https://ci.test/build",
			}}))
		case "/api/v1/repos/owner/repo/actions/runs":
			sawActions = true
			assert.Equal("abc", r.URL.Query().Get("head_sha"))
			assert.NoError(json.NewEncoder(w).Encode(map[string]any{
				"total_count": 1,
				"workflow_runs": []map[string]any{{
					"id": 41, "workflow_id": "ci.yml", "title": "CI", "status": "success",
					"commit_sha": "abc", "html_url": "https://codeberg.test/owner/repo/actions/runs/41",
				}},
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient("codeberg.test", "forgejo-token", WithBaseURLForTesting(server.URL))
	require.NoError(err)
	ref := platform.RepoRef{Owner: "owner", Name: "repo"}

	mrs, err := client.ListOpenMergeRequests(context.Background(), ref)
	require.NoError(err)
	issues, err := client.ListOpenIssues(context.Background(), ref)
	require.NoError(err)
	checks, err := client.ListCIChecks(context.Background(), ref, "abc")
	require.NoError(err)

	assert.True(sawPulls)
	assert.True(sawIssues)
	assert.True(sawStatuses)
	assert.True(sawActions)
	assert.Len(mrs, 1)
	assert.Len(issues, 1)
	assert.Len(checks, 2)
}

func TestClientMapsNotFoundResponsesToPlatformError(t *testing.T) {
	assert := Assert.New(t)
	require := Require.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal("/api/v1/repos/owner/repo/pulls/99", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		assert.NoError(json.NewEncoder(w).Encode(map[string]string{"message": "not found"}))
	}))
	defer server.Close()

	client, err := NewClient("codeberg.test", "forgejo-token", WithBaseURLForTesting(server.URL))
	require.NoError(err)

	_, err = client.GetMergeRequest(
		context.Background(),
		platform.RepoRef{Owner: "owner", Name: "repo"},
		99,
	)
	require.Error(err)
	assert.ErrorIs(err, platform.ErrNotFound)
}
