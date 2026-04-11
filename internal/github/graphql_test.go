package github

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
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
	gql.HeadRepository = &struct{ URL string }{URL: "https://github.com/o/r"}

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

func TestAdaptCheckRunURLSanitization(t *testing.T) {
	assert := Assert.New(t)

	safe := adaptCheckRun(&gqlCheckRunFields{
		Name:       "ci",
		Status:     "COMPLETED",
		Conclusion: "SUCCESS",
		DetailsURL: "https://ci.example.com/run/1",
	})
	assert.Equal("https://ci.example.com/run/1", safe.GetHTMLURL())

	unsafe := adaptCheckRun(&gqlCheckRunFields{
		Name:       "ci",
		Status:     "COMPLETED",
		Conclusion: "SUCCESS",
		DetailsURL: "javascript:alert(1)",
	})
	assert.Empty(unsafe.GetHTMLURL())
}

func TestGraphqlRateTransport(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)
	rt := NewRateTracker(d, "github.com", "graphql")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(30*time.Minute).Unix()))
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"data":{}}`))
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	transport := &graphqlRateTransport{
		base:        http.DefaultTransport,
		rateTracker: rt,
	}
	client := &http.Client{Transport: transport}

	req, err := http.NewRequest("POST", srv.URL, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(1, rt.RequestsThisHour())
	assert.Equal(4999, rt.Remaining())
	assert.Equal(5000, rt.RateLimit())
}

func TestConvertGQLPRCompleteness(t *testing.T) {
	assert := Assert.New(t)

	gql := gqlPR{
		Number:    1,
		Title:     "test",
		State:     "OPEN",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	gql.Author.Login = "user"
	bulk := convertGQLPR(&gql)
	assert.True(bulk.CommentsComplete)
	assert.True(bulk.ReviewsComplete)
	assert.True(bulk.CommitsComplete)
	assert.True(bulk.CIComplete)

	// Comments incomplete
	gql.Comments.PageInfo.HasNextPage = true
	bulk = convertGQLPR(&gql)
	assert.False(bulk.CommentsComplete)
	assert.True(bulk.ReviewsComplete)
}

func TestNormalizeBulkCI(t *testing.T) {
	assert := Assert.New(t)

	nameTest := "test"
	statusCompleted := "completed"
	conclusionSuccess := "success"
	detailsURL := "https://example.com"
	appName := "Actions"
	contextLint := "ci/lint"
	stateSuccess := "success"
	targetURL := "https://example.com/2"

	bulk := &BulkPR{
		CheckRuns: []*gh.CheckRun{
			{
				Name:       &nameTest,
				Status:     &statusCompleted,
				Conclusion: &conclusionSuccess,
				DetailsURL: &detailsURL,
				App:        &gh.App{Name: &appName},
			},
		},
		Statuses: []*gh.RepoStatus{
			{
				Context:   &contextLint,
				State:     &stateSuccess,
				TargetURL: &targetURL,
			},
		},
	}

	checks := normalizeBulkCI(bulk)
	require.Len(t, checks, 2)
	assert.Equal("test", checks[0].Name)
	assert.Equal("completed", checks[0].Status)
	assert.Equal("ci/lint", checks[1].Name)
	assert.Equal("completed", checks[1].Status)
}

func TestAdaptPRNilFields(t *testing.T) {
	assert := Assert.New(t)

	gql := gqlPR{
		Number:    1,
		Title:     "test",
		State:     "OPEN",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	// HeadRepository is nil
	pr := adaptPR(&gql)
	assert.Nil(pr.GetHead().GetRepo())
	assert.Nil(pr.MergedAt)
	assert.False(pr.GetMerged())
}

func TestAdaptIssue(t *testing.T) {
	assert := Assert.New(t)

	now := time.Now().UTC().Truncate(time.Second)
	closed := now.Add(-time.Hour)

	gql := gqlIssue{
		DatabaseId: 99999,
		Number:     10,
		Title:      "Bug report",
		State:      "OPEN",
		Body:       "Something broke",
		URL:        "https://github.com/o/r/issues/10",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	gql.Author.Login = "alice"
	gql.Labels.Nodes = []gqlLabel{
		{Name: "bug", Color: "d73a4a", Description: "Something broken", IsDefault: false},
	}
	gql.Comments.Nodes = []gqlComment{
		{DatabaseId: 501, Body: "I see this too", CreatedAt: now, UpdatedAt: now},
	}
	gql.Comments.Nodes[0].Author.Login = "bob"

	issue := adaptIssue(&gql)

	assert.Equal(int64(99999), issue.GetID())
	assert.Equal(10, issue.GetNumber())
	assert.Equal("Bug report", issue.GetTitle())
	assert.Equal("open", issue.GetState())
	assert.Equal("Something broke", issue.GetBody())
	assert.Equal("https://github.com/o/r/issues/10", issue.GetHTMLURL())
	assert.Equal("alice", issue.GetUser().GetLogin())
	require.Len(t, issue.Labels, 1)
	assert.Equal("bug", issue.Labels[0].GetName())
	assert.Equal("d73a4a", issue.Labels[0].GetColor())
	assert.Nil(issue.ClosedAt)

	// Test closed state
	gql.State = "CLOSED"
	gql.ClosedAt = &closed
	issue = adaptIssue(&gql)
	assert.Equal("closed", issue.GetState())
	require.NotNil(t, issue.ClosedAt)
	assert.Equal(closed, issue.ClosedAt.Time)
}

func TestAdaptIssueNilFields(t *testing.T) {
	assert := Assert.New(t)

	gql := gqlIssue{
		Number:    1,
		Title:     "minimal",
		State:     "OPEN",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	// Author empty, no labels, no ClosedAt
	issue := adaptIssue(&gql)
	assert.Empty(issue.GetUser().GetLogin())
	assert.Nil(issue.ClosedAt)
	assert.Empty(issue.Labels)
}

func TestConvertGQLIssue(t *testing.T) {
	assert := Assert.New(t)

	now := time.Now()
	gql := gqlIssue{
		DatabaseId: 1,
		Number:     5,
		Title:      "test",
		State:      "OPEN",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	gql.Author.Login = "user"

	// All complete (no next page)
	bulk := convertGQLIssue(&gql)
	assert.True(bulk.CommentsComplete)
	assert.NotNil(bulk.Issue)
	assert.Equal(5, bulk.Issue.GetNumber())
	assert.Empty(bulk.Comments)

	// Add comments with next page
	gql.Comments.Nodes = []gqlComment{
		{DatabaseId: 100, Body: "hello", CreatedAt: now, UpdatedAt: now},
	}
	gql.Comments.Nodes[0].Author.Login = "commenter"
	gql.Comments.PageInfo.HasNextPage = true

	bulk = convertGQLIssue(&gql)
	assert.False(bulk.CommentsComplete)
	require.Len(t, bulk.Comments, 1)
	assert.Equal("hello", bulk.Comments[0].GetBody())
}

func TestStateConversion(t *testing.T) {
	assert := Assert.New(t)
	assert.Equal("open", stateToREST("OPEN"))
	assert.Equal("closed", stateToREST("CLOSED"))
	assert.Equal("closed", stateToREST("MERGED"))
}

func TestMergeableConversion(t *testing.T) {
	assert := Assert.New(t)
	assert.Equal("clean", mergeableToREST("MERGEABLE"))
	assert.Equal("dirty", mergeableToREST("CONFLICTING"))
	assert.Equal("unknown", mergeableToREST("UNKNOWN"))
}

func TestNormalizeBulkCIPendingStatus(t *testing.T) {
	assert := Assert.New(t)

	contextDeploy := "ci/deploy"
	statePending := "pending"
	pendingURL := "https://example.com"

	bulk := &BulkPR{
		Statuses: []*gh.RepoStatus{
			{
				Context:   &contextDeploy,
				State:     &statePending,
				TargetURL: &pendingURL,
			},
		},
	}

	checks := normalizeBulkCI(bulk)
	require.Len(t, checks, 1)
	assert.Equal("ci/deploy", checks[0].Name)
	assert.Equal("in_progress", checks[0].Status)
	assert.Empty(checks[0].Conclusion)
}
