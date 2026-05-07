package gitealike

import (
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	Require "github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/platform"
)

func TestNormalizeRepositoryMapsSharedDTO(t *testing.T) {
	assert := Assert.New(t)
	require := Require.New(t)
	created := time.Date(2026, 5, 1, 2, 3, 4, 0, time.UTC)
	updated := created.Add(time.Hour)

	repo, err := NormalizeRepository(platform.KindForgejo, "codeberg.org", RepositoryDTO{
		ID:            42,
		Owner:         UserDTO{UserName: "forgejo"},
		Name:          "forgejo",
		FullName:      "forgejo/forgejo",
		HTMLURL:       "https://codeberg.org/forgejo/forgejo",
		CloneURL:      "https://codeberg.org/forgejo/forgejo.git",
		DefaultBranch: "forgejo",
		Private:       true,
		Archived:      true,
		Description:   "git forge",
		Created:       created,
		Updated:       updated,
	})
	require.NoError(err)

	assert.Equal(platform.KindForgejo, repo.Ref.Platform)
	assert.Equal("codeberg.org", repo.Ref.Host)
	assert.Equal("forgejo", repo.Ref.Owner)
	assert.Equal("forgejo", repo.Ref.Name)
	assert.Equal("forgejo/forgejo", repo.Ref.RepoPath)
	assert.Equal(int64(42), repo.Ref.PlatformID)
	assert.Equal("42", repo.Ref.PlatformExternalID)
	assert.Equal("https://codeberg.org/forgejo/forgejo", repo.Ref.WebURL)
	assert.Equal("https://codeberg.org/forgejo/forgejo.git", repo.Ref.CloneURL)
	assert.Equal("forgejo", repo.Ref.DefaultBranch)
	assert.Equal("git forge", repo.Description)
	assert.True(repo.Private)
	assert.True(repo.Archived)
	assert.Equal(created, repo.CreatedAt)
	assert.Equal(updated, repo.UpdatedAt)
}

func TestNormalizeMergeRequestIssueEventsAndArtifacts(t *testing.T) {
	assert := Assert.New(t)
	base := time.Date(2026, 5, 1, 2, 3, 4, 0, time.UTC)
	closed := base.Add(2 * time.Hour)
	repo := platform.RepoRef{
		Platform: platform.KindGitea,
		Host:     "gitea.com",
		Owner:    "gitea",
		Name:     "tea",
		RepoPath: "gitea/tea",
	}

	pr := NormalizePullRequest(repo, PullRequestDTO{
		ID:       100,
		Index:    7,
		HTMLURL:  "https://gitea.com/gitea/tea/pulls/7",
		Title:    "Add tea",
		User:     UserDTO{UserName: "alice", FullName: "Alice"},
		State:    "closed",
		Body:     "body",
		Head:     BranchDTO{Ref: "feature", SHA: "abc123", RepoCloneURL: "https://example/head.git"},
		Base:     BranchDTO{Ref: "main", SHA: "def456"},
		Labels:   []LabelDTO{{ID: 1, Name: "kind/feature", Color: "00ff00", Description: "feature", IsDefault: true}},
		Created:  base,
		Updated:  base.Add(time.Hour),
		Merged:   true,
		MergedAt: &closed,
		Closed:   &closed,
	})
	assert.Equal("merged", pr.State)
	assert.Equal(7, pr.Number)
	assert.Equal("alice", pr.Author)
	assert.Equal("Alice", pr.AuthorDisplayName)
	assert.Equal("feature", pr.HeadBranch)
	assert.Equal("abc123", pr.HeadSHA)
	assert.Equal("main", pr.BaseBranch)
	assert.Equal("def456", pr.BaseSHA)
	assert.Equal("https://example/head.git", pr.HeadRepoCloneURL)
	assert.Equal("kind/feature", pr.Labels[0].Name)
	assert.Equal(&closed, pr.MergedAt)
	assert.Equal(&closed, pr.ClosedAt)

	issue := NormalizeIssue(repo, IssueDTO{
		ID:       200,
		Index:    9,
		HTMLURL:  "https://gitea.com/gitea/tea/issues/9",
		Title:    "Bug",
		User:     UserDTO{UserName: "bob"},
		State:    "closed",
		Body:     "issue body",
		Comments: 3,
		Labels:   []LabelDTO{{ID: 2, Name: "bug"}},
		Created:  base,
		Updated:  base.Add(time.Hour),
		Closed:   &closed,
	})
	assert.Equal("closed", issue.State)
	assert.Equal(9, issue.Number)
	assert.Equal("bob", issue.Author)
	assert.Equal(3, issue.CommentCount)
	assert.Equal(&closed, issue.ClosedAt)

	mrEvents := NormalizeMergeRequestEvents(
		platform.KindGitea,
		repo,
		7,
		[]CommentDTO{{ID: 300, User: UserDTO{UserName: "carol"}, Body: "comment", Created: base}},
		[]ReviewDTO{{ID: 301, User: UserDTO{UserName: "dave"}, State: "APPROVED", Body: "review", Submitted: base.Add(time.Minute)}},
		[]CommitDTO{{SHA: "abc123", AuthorName: "eve", Message: "commit", Created: base.Add(2 * time.Minute)}},
	)
	assert.Len(mrEvents, 3)
	assert.Equal("issue_comment", mrEvents[0].EventType)
	assert.Equal("review", mrEvents[1].EventType)
	assert.Equal("commit", mrEvents[2].EventType)
	assert.Contains(mrEvents[0].DedupeKey, "gitea/gitea.com/gitea/tea")

	issueEvents := NormalizeIssueComments(
		platform.KindGitea,
		repo,
		9,
		[]CommentDTO{{ID: 400, User: UserDTO{UserName: "frank"}, Body: "issue comment", Created: base}},
	)
	assert.Len(issueEvents, 1)
	assert.Equal("issue_comment", issueEvents[0].EventType)
	assert.Equal("frank", issueEvents[0].Author)

	release := NormalizeRelease(repo, ReleaseDTO{
		ID:          500,
		TagName:     "v1.0.0",
		Title:       "One",
		HTMLURL:     "https://gitea.com/gitea/tea/releases/v1.0.0",
		Target:      "main",
		Prerelease:  true,
		PublishedAt: &closed,
		CreatedAt:   base,
	})
	assert.Equal("v1.0.0", release.TagName)
	assert.Equal("One", release.Name)
	assert.Equal(&closed, release.PublishedAt)

	tag := NormalizeTag(repo, TagDTO{Name: "v1.0.0", Commit: CommitDTO{SHA: "abc123"}})
	assert.Equal("v1.0.0", tag.Name)
	assert.Equal("abc123", tag.SHA)
}

func TestNormalizeStatusesMapsCommitStatusesAndActionRuns(t *testing.T) {
	assert := Assert.New(t)
	require := Require.New(t)
	started := time.Date(2026, 5, 1, 2, 3, 4, 0, time.UTC)
	stopped := started.Add(time.Minute)
	repo := platform.RepoRef{Platform: platform.KindForgejo, Host: "codeberg.org", RepoPath: "forgejo/forgejo"}

	checks := NormalizeStatuses(repo, []StatusDTO{
		{ID: 1, Context: "ci/success", State: "success", TargetURL: "https://ci/success", Description: "ok", Created: started, Updated: stopped},
		{ID: 2, Context: "ci/pending", State: "pending", TargetURL: "https://ci/pending", Created: started},
		{ID: 3, Context: "ci/error", State: "error", TargetURL: "https://ci/error", Created: started},
	}, []ActionRunDTO{
		{ID: 4, WorkflowID: "build", Title: "Build", Status: "failure", CommitSHA: "abc123", HTMLURL: "https://actions/build", Started: &started, Stopped: &stopped, NeedApproval: true},
	})

	require.Len(checks, 4)
	assert.Equal("ci/success", checks[0].Name)
	assert.Equal("completed", checks[0].Status)
	assert.Equal("success", checks[0].Conclusion)
	assert.Equal("pending", checks[1].Status)
	assert.Empty(checks[1].Conclusion)
	assert.Equal("failure", checks[2].Conclusion)
	assert.Equal("Build", checks[3].Name)
	assert.Equal("action", checks[3].App)
	assert.Equal(&started, checks[3].StartedAt)
	assert.Equal(&stopped, checks[3].CompletedAt)
}

func TestSharedHelpersNormalizeStateDedupeAndPagination(t *testing.T) {
	assert := Assert.New(t)

	assert.Equal("open", NormalizeState("opened"))
	assert.Equal("closed", NormalizeState("closed"))
	assert.Equal("merged", NormalizeState("merged"))
	assert.Equal("custom", NormalizeState(" custom "))
	assert.Equal("owner/repo", OwnerRepoPath("owner", "repo"))
	assert.Equal(0, NextPage(0))
	assert.Equal(3, NextPage(3))
	assert.Equal(
		"forgejo/codeberg.org/forgejo/forgejo/mr/7/issue_comment/123",
		NoteDedupeKey(platform.KindForgejo, "codeberg.org", "forgejo/forgejo", "mr", 7, "issue_comment", "123"),
	)
}
