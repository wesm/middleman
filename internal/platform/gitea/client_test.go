package gitea

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
)

func TestClientLooksUpRepositoryAndSendsToken(t *testing.T) {
	assert := Assert.New(t)
	require := Require.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(http.MethodGet, r.Method)
		assert.Equal("/api/v1/repos/owner/repo", r.URL.Path)
		assert.Equal("token gitea-token", r.Header.Get("Authorization"))
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
		"gitea.test",
		"gitea-token",
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
		"gitea.test",
		"gitea-token",
		WithBaseURLForTesting(server.URL),
		WithForegroundTimeoutForTesting(20*time.Millisecond),
	)
	require.NoError(err)

	_, err = client.transport.getRepositoryRaw(context.Background(), "owner", "repo")
	require.Error(err)
}

func TestTransportGetRepositoryRawCancelsInFlightRequest(t *testing.T) {
	assert := Assert.New(t)
	require := Require.New(t)
	requestStarted := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal("/api/v1/repos/owner/repo", r.URL.Path)
		close(requestStarted)
		<-r.Context().Done()
	}))
	defer server.Close()

	client, err := NewClient(
		"gitea.test",
		"gitea-token",
		WithBaseURLForTesting(server.URL),
		WithForegroundTimeoutForTesting(time.Minute),
	)
	require.NoError(err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		_, err := client.transport.getRepositoryRaw(ctx, "owner", "repo")
		done <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		require.FailNow("request did not start")
	}
	cancel()

	select {
	case err := <-done:
		require.ErrorIs(err, context.Canceled)
	case <-time.After(time.Second):
		require.FailNow("request was not canceled")
	}
}

func TestTransportGetRepositoryRawCancelsWhileWaitingForRequestContext(t *testing.T) {
	assert := Assert.New(t)
	require := Require.New(t)
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal("/api/v1/repos/owner/repo", r.URL.Path)
		close(requestStarted)
		<-releaseRequest
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(json.NewEncoder(w).Encode(map[string]any{
			"id":        1,
			"name":      "repo",
			"full_name": "owner/repo",
			"owner": map[string]any{
				"id":    2,
				"login": "owner",
			},
		}))
	}))
	defer server.Close()

	client, err := NewClient(
		"gitea.test",
		"gitea-token",
		WithBaseURLForTesting(server.URL),
		WithForegroundTimeoutForTesting(time.Minute),
	)
	require.NoError(err)
	firstDone := make(chan error, 1)
	go func() {
		_, err := client.transport.getRepositoryRaw(context.Background(), "owner", "repo")
		firstDone <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		require.FailNow("request did not start")
	}

	waitingCtx, cancelWaiting := context.WithCancel(context.Background())
	waitingDone := make(chan error, 1)
	go func() {
		_, err := client.transport.getRepositoryRaw(waitingCtx, "owner", "repo")
		waitingDone <- err
	}()
	cancelWaiting()

	select {
	case err := <-waitingDone:
		require.ErrorIs(err, context.Canceled)
	case <-time.After(time.Second):
		require.FailNow("waiting request was not canceled")
	}

	close(releaseRequest)
	select {
	case err := <-firstDone:
		require.NoError(err)
	case <-time.After(time.Second):
		require.FailNow("first request did not finish")
	}
}

func TestClientProviderIdentityHasNoReadCapabilitiesYet(t *testing.T) {
	assert := Assert.New(t)
	require := Require.New(t)
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	client, err := NewClient(
		"gitea.test",
		"gitea-token",
		WithBaseURLForTesting(server.URL),
	)
	require.NoError(err)

	assert.Equal(platform.KindGitea, client.Platform())
	assert.Equal("gitea.test", client.Host())
	assert.Equal(platform.Capabilities{}, client.Capabilities())
}
