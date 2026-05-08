package gitealike

import (
	"context"
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	Require "github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/platform"
)

func TestProviderCapabilitiesEnableSharedReadBehavior(t *testing.T) {
	provider := NewProvider(platform.KindForgejo, "codeberg.org", &fakeTransport{}, WithReadActions())

	Assert.Equal(t, platform.Capabilities{
		ReadRepositories:  true,
		ReadMergeRequests: true,
		ReadIssues:        true,
		ReadComments:      true,
		ReadReleases:      true,
		ReadCI:            true,
	}, provider.Capabilities())
}

func TestProviderPaginatesAndNormalizesReadMethods(t *testing.T) {
	assert := Assert.New(t)
	require := Require.New(t)
	base := time.Date(2026, 5, 1, 2, 3, 4, 0, time.UTC)
	ref := platform.RepoRef{
		Platform: platform.KindForgejo,
		Host:     "codeberg.org",
		Owner:    "forgejo",
		Name:     "forgejo",
		RepoPath: "forgejo/forgejo",
	}
	transport := &fakeTransport{
		repo: RepositoryDTO{
			ID:            1,
			Owner:         UserDTO{UserName: "forgejo"},
			Name:          "forgejo",
			FullName:      "forgejo/forgejo",
			DefaultBranch: "main",
			Created:       base,
			Updated:       base,
		},
		userRepos: [][]RepositoryDTO{
			{{ID: 2, Owner: UserDTO{UserName: "forgejo"}, Name: "one", FullName: "forgejo/one"}},
			{{ID: 3, Owner: UserDTO{UserName: "forgejo"}, Name: "two", FullName: "forgejo/two"}},
		},
		pulls: [][]PullRequestDTO{
			{{ID: 4, Index: 1, Title: "one", User: UserDTO{UserName: "alice"}, State: "open", Created: base, Updated: base}},
			{{ID: 5, Index: 2, Title: "two", User: UserDTO{UserName: "bob"}, State: "open", Created: base, Updated: base}},
		},
		issues: [][]IssueDTO{
			{
				{ID: 6, Index: 3, Title: "issue", User: UserDTO{UserName: "carol"}, State: "open", Created: base, Updated: base},
				{ID: 7, Index: 4, Title: "pull duplicate", User: UserDTO{UserName: "dan"}, State: "open", Created: base, Updated: base, IsPullRequest: true},
			},
		},
		releases: [][]ReleaseDTO{{{ID: 8, TagName: "v1", Title: "one", CreatedAt: base}}},
		tags:     [][]TagDTO{{{Name: "v1", Commit: CommitDTO{SHA: "abc"}}}},
		statuses: [][]StatusDTO{{{ID: 9, Context: "ci", State: "success", Created: base}}},
	}
	provider := NewProvider(platform.KindForgejo, "codeberg.org", transport)

	repo, err := provider.GetRepository(context.Background(), ref)
	require.NoError(err)
	assert.Equal("forgejo/forgejo", repo.Ref.RepoPath)

	repos, err := provider.ListRepositories(context.Background(), "forgejo", platform.RepositoryListOptions{})
	require.NoError(err)
	assert.Equal([]string{"one", "two"}, []string{repos[0].Ref.Name, repos[1].Ref.Name})
	assert.Equal([]int{1, 2}, transport.userRepoPages)

	mrs, err := provider.ListOpenMergeRequests(context.Background(), ref)
	require.NoError(err)
	assert.Equal([]int{1, 2}, []int{mrs[0].Number, mrs[1].Number})
	assert.Equal([]int{1, 2}, transport.pullPages)

	issues, err := provider.ListOpenIssues(context.Background(), ref)
	require.NoError(err)
	require.Len(issues, 1)
	assert.Equal(3, issues[0].Number)

	releases, err := provider.ListReleases(context.Background(), ref)
	require.NoError(err)
	require.Len(releases, 1)
	assert.Equal("v1", releases[0].TagName)

	tags, err := provider.ListTags(context.Background(), ref)
	require.NoError(err)
	require.Len(tags, 1)
	assert.Equal("abc", tags[0].SHA)

	checks, err := provider.ListCIChecks(context.Background(), ref, "abc")
	require.NoError(err)
	require.Len(checks, 1)
	assert.Equal("success", checks[0].Conclusion)
}

func TestCollectPagesRejectsNonAdvancingPagination(t *testing.T) {
	assert := Assert.New(t)
	require := Require.New(t)
	_, err := collectPages(context.Background(), func(opts PageOptions) ([]int, Page, error) {
		return []int{opts.Page}, Page{Next: opts.Page}, nil
	})
	require.Error(err)
	assert.Contains(err.Error(), "did not advance")
}

func TestCollectPagesRejectsExcessivePagination(t *testing.T) {
	assert := Assert.New(t)
	require := Require.New(t)
	_, err := collectPages(context.Background(), func(opts PageOptions) ([]int, Page, error) {
		return []int{opts.Page}, Page{Next: opts.Page + 1}, nil
	})
	require.Error(err)
	assert.Contains(err.Error(), "exceeded")
}

func TestProviderFallsBackFromUserToOrgRepositoryImport(t *testing.T) {
	assert := Assert.New(t)
	require := Require.New(t)
	transport := &fakeTransport{
		userRepoErr: &HTTPError{StatusCode: 404, Message: "user missing"},
		orgRepos: [][]RepositoryDTO{
			{{ID: 10, Owner: UserDTO{UserName: "org"}, Name: "repo", FullName: "org/repo"}},
		},
	}
	provider := NewProvider(platform.KindGitea, "gitea.com", transport)

	repos, err := provider.ListRepositories(context.Background(), "org", platform.RepositoryListOptions{})
	require.NoError(err)

	require.Len(repos, 1)
	assert.Equal("org/repo", repos[0].Ref.RepoPath)
	assert.Equal([]int{1}, transport.userRepoPages)
	assert.Equal([]int{1}, transport.orgRepoPages)
}

func TestProviderMapsHTTPStatusErrorsToTypedPlatformErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		target     error
		code       platform.PlatformErrorCode
	}{
		{"unauthorized", 401, platform.ErrPermissionDenied, platform.ErrCodePermissionDenied},
		{"forbidden", 403, platform.ErrPermissionDenied, platform.ErrCodePermissionDenied},
		{"not found", 404, platform.ErrNotFound, platform.ErrCodeNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := Assert.New(t)
			require := Require.New(t)
			provider := NewProvider(platform.KindForgejo, "codeberg.org", &fakeTransport{
				repoErr: &HTTPError{StatusCode: tt.statusCode, Message: "failed"},
			})

			_, err := provider.GetRepository(context.Background(), platform.RepoRef{
				Owner: "forgejo",
				Name:  "forgejo",
			})

			require.Error(err)
			require.ErrorIs(err, tt.target)
			var platformErr *platform.Error
			require.ErrorAs(err, &platformErr)
			assert.Equal(tt.code, platformErr.Code)
			assert.Equal(platform.KindForgejo, platformErr.Provider)
			assert.Equal("codeberg.org", platformErr.PlatformHost)
		})
	}
}

type fakeTransport struct {
	repo        RepositoryDTO
	repoErr     error
	userRepos   [][]RepositoryDTO
	userRepoErr error
	orgRepos    [][]RepositoryDTO
	orgRepoErr  error
	pulls       [][]PullRequestDTO
	issues      [][]IssueDTO
	releases    [][]ReleaseDTO
	tags        [][]TagDTO
	statuses    [][]StatusDTO

	userRepoPages []int
	orgRepoPages  []int
	pullPages     []int
}

func (t *fakeTransport) GetRepository(context.Context, string, string) (RepositoryDTO, error) {
	return t.repo, t.repoErr
}

func (t *fakeTransport) ListUserRepositories(
	_ context.Context,
	_ string,
	opts PageOptions,
) ([]RepositoryDTO, Page, error) {
	t.userRepoPages = append(t.userRepoPages, opts.Page)
	if t.userRepoErr != nil {
		return nil, Page{}, t.userRepoErr
	}
	return pageFor(t.userRepos, opts.Page)
}

func (t *fakeTransport) ListOrgRepositories(
	_ context.Context,
	_ string,
	opts PageOptions,
) ([]RepositoryDTO, Page, error) {
	t.orgRepoPages = append(t.orgRepoPages, opts.Page)
	if t.orgRepoErr != nil {
		return nil, Page{}, t.orgRepoErr
	}
	return pageFor(t.orgRepos, opts.Page)
}

func (t *fakeTransport) ListOpenPullRequests(
	_ context.Context,
	_ platform.RepoRef,
	opts PageOptions,
) ([]PullRequestDTO, Page, error) {
	t.pullPages = append(t.pullPages, opts.Page)
	return pageFor(t.pulls, opts.Page)
}

func (t *fakeTransport) GetPullRequest(context.Context, platform.RepoRef, int) (PullRequestDTO, error) {
	return PullRequestDTO{}, nil
}

func (t *fakeTransport) ListPullRequestComments(context.Context, platform.RepoRef, int, PageOptions) ([]CommentDTO, Page, error) {
	return nil, Page{}, nil
}

func (t *fakeTransport) ListPullRequestReviews(context.Context, platform.RepoRef, int, PageOptions) ([]ReviewDTO, Page, error) {
	return nil, Page{}, nil
}

func (t *fakeTransport) ListPullRequestCommits(context.Context, platform.RepoRef, int, PageOptions) ([]CommitDTO, Page, error) {
	return nil, Page{}, nil
}

func (t *fakeTransport) ListOpenIssues(_ context.Context, _ platform.RepoRef, opts PageOptions) ([]IssueDTO, Page, error) {
	return pageFor(t.issues, opts.Page)
}

func (t *fakeTransport) GetIssue(context.Context, platform.RepoRef, int) (IssueDTO, error) {
	return IssueDTO{}, nil
}

func (t *fakeTransport) ListIssueComments(context.Context, platform.RepoRef, int, PageOptions) ([]CommentDTO, Page, error) {
	return nil, Page{}, nil
}

func (t *fakeTransport) ListReleases(_ context.Context, _ platform.RepoRef, opts PageOptions) ([]ReleaseDTO, Page, error) {
	return pageFor(t.releases, opts.Page)
}

func (t *fakeTransport) ListTags(_ context.Context, _ platform.RepoRef, opts PageOptions) ([]TagDTO, Page, error) {
	return pageFor(t.tags, opts.Page)
}

func (t *fakeTransport) ListStatuses(_ context.Context, _ platform.RepoRef, _ string, opts PageOptions) ([]StatusDTO, Page, error) {
	return pageFor(t.statuses, opts.Page)
}

func pageFor[T any](pages [][]T, page int) ([]T, Page, error) {
	if page < 1 || page > len(pages) {
		return nil, Page{}, nil
	}
	next := 0
	if page < len(pages) {
		next = page + 1
	}
	return pages[page-1], Page{Next: next}, nil
}
