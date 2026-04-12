package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
	"github.com/wesm/middleman/internal/server"
	"github.com/wesm/middleman/internal/testutil"
)

func TestWriteServerInfoFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server-info.json")
	info := e2eServerInfo{
		Host:    "127.0.0.1",
		Port:    43123,
		BaseURL: "http://127.0.0.1:43123",
		PID:     4242,
	}

	require.NoError(t, writeServerInfoFile(path, info))

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	var got e2eServerInfo
	require.NoError(t, json.Unmarshal(content, &got))
	assert := Assert.New(t)
	assert.Equal(info, got)
}

func TestCleanupServerInfoFileRemovesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server-info.json")
	require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o644))

	cleanupServerInfoFile(path)

	_, err := os.Stat(path)
	assert := Assert.New(t)
	assert.ErrorIs(err, os.ErrNotExist)
}

func TestPatchFixturePRSHAsUpdatesOpenPRs(t *testing.T) {
	openPR := &gh.PullRequest{
		Number: new(1),
		Head:   &gh.PullRequestBranch{},
		Base:   &gh.PullRequestBranch{},
	}
	fc := &testutil.FixtureClient{
		OpenPRs: map[string][]*gh.PullRequest{
			"acme/widgets": {openPR},
		},
	}

	patchFixturePRSHAs(fc, "acme", "widgets", 1, "head-sha", "base-sha")

	assert := Assert.New(t)
	assert.Equal("head-sha", openPR.GetHead().GetSHA())
	assert.Equal("base-sha", openPR.GetBase().GetSHA())
}

func TestPatchFixturePRSHAsUpdatesLookupPRs(t *testing.T) {
	lookupPR := &gh.PullRequest{
		Number: new(1),
		Head:   &gh.PullRequestBranch{},
		Base:   &gh.PullRequestBranch{},
	}
	fc := &testutil.FixtureClient{
		PRs: map[string][]*gh.PullRequest{
			"acme/widgets": {lookupPR},
		},
	}

	patchFixturePRSHAs(fc, "acme", "widgets", 1, "head-sha", "base-sha")

	assert := Assert.New(t)
	assert.Equal("head-sha", lookupPR.GetHead().GetSHA())
	assert.Equal("base-sha", lookupPR.GetBase().GetSHA())
}

// TestDefaultRoborevEndpointIsUnbindable pins the e2e server's
// roborev flag default to a loopback address with a privileged
// port. Reverting this default would re-introduce silent forwarding
// to a real local roborev daemon at 127.0.0.1:7373 during direct
// playwright runs.
func TestDefaultRoborevEndpointIsUnbindable(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	u, err := url.Parse(defaultRoborevEndpoint)
	require.NoError(err)
	assert.Equal("127.0.0.1", u.Hostname(),
		"default roborev endpoint must be loopback")

	port, err := strconv.Atoi(u.Port())
	require.NoError(err)
	assert.Less(port, 1024,
		"default roborev port must be privileged so it cannot be "+
			"silently bound by an unrelated developer process")
	assert.Positive(port)
}

// TestDefaultRoborevEndpointFailsClosedThroughProxy is the
// behavioral guard the test review asked for: it boots a real
// middleman server with the e2e server's default roborev endpoint
// and verifies that /api/roborev/api/status returns a closed-fail
// 502 with the proxy's "not reachable" error JSON, instead of
// silently forwarding the probe to a real local daemon.
func TestDefaultRoborevEndpointFailsClosedThroughProxy(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{}, database, nil, nil,
		time.Minute, nil, nil,
	)
	t.Cleanup(syncer.Stop)

	cfg := &config.Config{
		Repos: []config.Repo{},
	}
	cfg.Roborev.Endpoint = defaultRoborevEndpoint

	srv := server.New(database, syncer, nil, "/", cfg, server.ServerOptions{})

	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/roborev/api/status")
	require.NoError(err)
	defer resp.Body.Close()

	assert.Equal(http.StatusBadGateway, resp.StatusCode,
		"default roborev endpoint must fail closed through the proxy")

	body, err := io.ReadAll(resp.Body)
	require.NoError(err)
	var payload map[string]string
	require.NoError(json.Unmarshal(body, &payload))
	assert.Contains(payload["error"], "roborev daemon is not reachable")
}
