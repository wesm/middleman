package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	gh "github.com/google/go-github/v84/github"
	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestPatchFixturePRSHAsUpdatesOpenAndAllPRs(t *testing.T) {
	openPR := &gh.PullRequest{
		Number: new(1),
		Head:   &gh.PullRequestBranch{},
		Base:   &gh.PullRequestBranch{},
	}
	allPR := &gh.PullRequest{
		Number: new(1),
		Head:   &gh.PullRequestBranch{},
		Base:   &gh.PullRequestBranch{},
	}
	fc := &testutil.FixtureClient{
		OpenPRs: map[string][]*gh.PullRequest{
			"acme/widgets": {openPR},
		},
		PRs: map[string][]*gh.PullRequest{
			"acme/widgets": {allPR},
		},
	}

	patchFixturePRSHAs(fc, "acme", "widgets", 1, "head-sha", "base-sha")

	assert := Assert.New(t)
	assert.Equal("head-sha", openPR.GetHead().GetSHA())
	assert.Equal("base-sha", openPR.GetBase().GetSHA())
	assert.Equal("head-sha", allPR.GetHead().GetSHA())
	assert.Equal("base-sha", allPR.GetBase().GetSHA())
}
