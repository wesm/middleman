package forgejo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	Assert "github.com/stretchr/testify/assert"
	Require "github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/platform"
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

func TestClientProviderIdentityHasNoReadCapabilitiesYet(t *testing.T) {
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
	assert.Equal(platform.Capabilities{}, client.Capabilities())
}
