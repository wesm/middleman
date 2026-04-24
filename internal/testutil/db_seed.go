package testutil

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/db"
)

type MergeRequestSeed struct {
	Host, Owner, Name string
	Number            int
	Title, Author     string
	State             string
	HeadBranch        string
	BaseBranch        string
	HeadRepoCloneURL  string
	PlatformHeadSHA   string
	PlatformBaseSHA   string
	Labels            []db.Label
}

type IssueSeed struct {
	Host, Owner, Name string
	Number            int
	Title, Author     string
	State             string
}

func SeedRepo(t *testing.T, database *db.DB, host, owner, name string) int64 {
	t.Helper()
	id, err := database.UpsertRepo(t.Context(), host, owner, name)
	require.NoError(t, err)
	return id
}

func SeedMergeRequest(t *testing.T, database *db.DB, seed MergeRequestSeed) int64 {
	t.Helper()
	seed = normalizeMRSeed(seed)
	repoID := SeedRepo(t, database, seed.Host, seed.Owner, seed.Name)
	now := time.Now().UTC().Truncate(time.Second)
	mr := &db.MergeRequest{
		RepoID:           repoID,
		PlatformID:       repoID*10000 + int64(seed.Number),
		Number:           seed.Number,
		URL:              fmt.Sprintf("https://%s/%s/%s/pull/%d", seed.Host, seed.Owner, seed.Name, seed.Number),
		Title:            seed.Title,
		Author:           seed.Author,
		State:            seed.State,
		HeadBranch:       seed.HeadBranch,
		BaseBranch:       seed.BaseBranch,
		HeadRepoCloneURL: seed.HeadRepoCloneURL,
		PlatformHeadSHA:  seed.PlatformHeadSHA,
		PlatformBaseSHA:  seed.PlatformBaseSHA,
		CreatedAt:        now,
		UpdatedAt:        now,
		LastActivityAt:   now,
	}
	id, err := database.UpsertMergeRequest(t.Context(), mr)
	require.NoError(t, err)
	if len(seed.Labels) > 0 {
		require.NoError(t, database.ReplaceMergeRequestLabels(t.Context(), repoID, id, seed.Labels))
	}
	return id
}

func SeedIssue(t *testing.T, database *db.DB, seed IssueSeed) int64 {
	t.Helper()
	seed = normalizeIssueSeed(seed)
	repoID := SeedRepo(t, database, seed.Host, seed.Owner, seed.Name)
	now := time.Now().UTC().Truncate(time.Second)
	issue := &db.Issue{
		RepoID:         repoID,
		PlatformID:     repoID*10000 + int64(seed.Number),
		Number:         seed.Number,
		URL:            fmt.Sprintf("https://%s/%s/%s/issues/%d", seed.Host, seed.Owner, seed.Name, seed.Number),
		Title:          seed.Title,
		Author:         seed.Author,
		State:          seed.State,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	}
	id, err := database.UpsertIssue(t.Context(), issue)
	require.NoError(t, err)
	return id
}

func normalizeMRSeed(seed MergeRequestSeed) MergeRequestSeed {
	if seed.Host == "" {
		seed.Host = "github.com"
	}
	if seed.Owner == "" {
		seed.Owner = "acme"
	}
	if seed.Name == "" {
		seed.Name = "widget"
	}
	if seed.Number == 0 {
		seed.Number = 1
	}
	if seed.Title == "" {
		seed.Title = fmt.Sprintf("PR %d", seed.Number)
	}
	if seed.Author == "" {
		seed.Author = "author"
	}
	if seed.State == "" {
		seed.State = "open"
	}
	if seed.HeadBranch == "" {
		seed.HeadBranch = "feature"
	}
	if seed.BaseBranch == "" {
		seed.BaseBranch = "main"
	}
	return seed
}

func normalizeIssueSeed(seed IssueSeed) IssueSeed {
	if seed.Host == "" {
		seed.Host = "github.com"
	}
	if seed.Owner == "" {
		seed.Owner = "acme"
	}
	if seed.Name == "" {
		seed.Name = "widget"
	}
	if seed.Number == 0 {
		seed.Number = 1
	}
	if seed.Title == "" {
		seed.Title = fmt.Sprintf("Issue %d", seed.Number)
	}
	if seed.Author == "" {
		seed.Author = "author"
	}
	if seed.State == "" {
		seed.State = "open"
	}
	return seed
}
