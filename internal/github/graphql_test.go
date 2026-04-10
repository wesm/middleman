package github

import (
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdaptPR(t *testing.T) {
	assert := Assert.New(t)

	now := time.Now().UTC().Truncate(time.Second)
	merged := now.Add(-time.Hour)

	gql := gqlPR{
		DatabaseId:     12345,
		Number:         42,
		Title:          "Fix bug",
		State:          "OPEN",
		IsDraft:        true,
		Body:           "Fixes #1",
		URL:            "https://github.com/o/r/pull/42",
		Additions:      10,
		Deletions:      3,
		Mergeable:      "MERGEABLE",
		ReviewDecision: "APPROVED",
		HeadRefName:    "fix-branch",
		BaseRefName:    "main",
		HeadRefOid:     "abc123",
		BaseRefOid:     "def456",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	gql.Author.Login = "alice"
	gql.MergedAt = &merged
	gql.HeadRepository = &struct{ URL string }{URL: "https://github.com/o/r.git"}

	pr := adaptPR(&gql)

	assert.Equal(int64(12345), pr.GetID())
	assert.Equal(42, pr.GetNumber())
	assert.Equal("Fix bug", pr.GetTitle())
	assert.Equal("open", pr.GetState())
	assert.True(pr.GetDraft())
	assert.Equal("Fixes #1", pr.GetBody())
	assert.Equal("https://github.com/o/r/pull/42", pr.GetHTMLURL())
	assert.Equal(10, pr.GetAdditions())
	assert.Equal(3, pr.GetDeletions())
	assert.Equal("alice", pr.GetUser().GetLogin())
	assert.Equal("fix-branch", pr.GetHead().GetRef())
	assert.Equal("main", pr.GetBase().GetRef())
	assert.Equal("abc123", pr.GetHead().GetSHA())
	assert.Equal("def456", pr.GetBase().GetSHA())
	assert.Equal("https://github.com/o/r.git", pr.GetHead().GetRepo().GetCloneURL())
	assert.Equal("clean", pr.GetMergeableState())
	require.NotNil(t, pr.MergedAt)
	assert.True(pr.GetMerged())
}

func TestAdaptComment(t *testing.T) {
	assert := Assert.New(t)
	now := time.Now().UTC().Truncate(time.Second)

	gql := gqlComment{
		DatabaseId: 100,
		Body:       "LGTM",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	gql.Author.Login = "bob"

	c := adaptComment(&gql)

	assert.Equal(int64(100), c.GetID())
	assert.Equal("LGTM", c.GetBody())
	assert.Equal("bob", c.GetUser().GetLogin())
}

func TestAdaptReview(t *testing.T) {
	assert := Assert.New(t)
	now := time.Now().UTC().Truncate(time.Second)

	gql := gqlReview{
		DatabaseId:  200,
		Body:        "Looks good",
		State:       "APPROVED",
		SubmittedAt: now,
	}
	gql.Author.Login = "carol"

	r := adaptReview(&gql)

	assert.Equal(int64(200), r.GetID())
	assert.Equal("Looks good", r.GetBody())
	assert.Equal("APPROVED", r.GetState())
	assert.Equal("carol", r.GetUser().GetLogin())
}

func TestAdaptCommit(t *testing.T) {
	assert := Assert.New(t)
	now := time.Now().UTC().Truncate(time.Second)

	gql := gqlCommitNode{
		Commit: gqlCommit{
			OID:     "sha123",
			Message: "fix: something",
		},
	}
	gql.Commit.Author.Name = "Dave"
	gql.Commit.Author.Date = now
	gql.Commit.Author.User = &struct{ Login string }{Login: "dave"}

	c := adaptCommit(&gql)

	assert.Equal("sha123", c.GetSHA())
	assert.Equal("fix: something", c.GetCommit().GetMessage())
	assert.Equal("Dave", c.GetCommit().GetAuthor().GetName())
	assert.Equal("dave", c.GetAuthor().GetLogin())
}

func TestAdaptCheckContext(t *testing.T) {
	assert := Assert.New(t)

	contexts := []gqlCheckContext{
		{
			Typename: "CheckRun",
			CheckRun: gqlCheckRunFields{
				Name:       "ci/test",
				Status:     "COMPLETED",
				Conclusion: "SUCCESS",
				DetailsURL: "https://example.com/1",
			},
		},
		{
			Typename: "StatusContext",
			StatusContext: gqlStatusContextFields{
				Context:   "ci/lint",
				State:     "SUCCESS",
				TargetURL: "https://example.com/2",
			},
		},
	}
	contexts[0].CheckRun.CheckSuite.App.Name = "GitHub Actions"

	checks, statuses := splitCheckContexts(contexts)

	require.Len(t, checks, 1)
	assert.Equal("ci/test", checks[0].GetName())
	assert.Equal("completed", checks[0].GetStatus())
	assert.Equal("success", checks[0].GetConclusion())
	assert.Equal("GitHub Actions", checks[0].GetApp().GetName())

	require.Len(t, statuses, 1)
	assert.Equal("ci/lint", statuses[0].GetContext())
	assert.Equal("success", statuses[0].GetState())
}
