package repoidentity

import (
	"context"
	"testing"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/db"
)

func TestLookupRepoRequiresPlatformHost(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	store := &fakeStore{
		repos: []db.Repo{
			{ID: 1, PlatformHost: "github.com", Owner: "acme", Name: "widget"},
		},
	}
	module := New(store)

	repo, err := module.LookupRepo(t.Context(), Ref{
		Owner: " Acme ",
		Name:  "Widget",
	})
	require.ErrorIs(err, ErrMissingPlatformHost)
	assert.Nil(repo)
	assert.Zero(store.listReposByOwnerNameCalls)

	repo, err = module.LookupRepo(t.Context(), Ref{
		Owner:        " Acme ",
		Name:         "Widget",
		PlatformHost: "GITHUB.COM",
	})
	require.NoError(err)
	require.NotNil(repo)
	assert.EqualValues(1, repo.ID)
}

func TestLookupIssueUsesRepoIDAfterHostSelection(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	store := &fakeStore{
		repos: []db.Repo{
			{ID: 1, PlatformHost: "github.com", Owner: "acme", Name: "widget"},
			{ID: 2, PlatformHost: "ghe.example.com", Owner: "acme", Name: "widget"},
		},
		issues: map[int64]map[int]*db.Issue{
			1: {5: {ID: 101, RepoID: 1, Number: 5}},
			2: {5: {ID: 202, RepoID: 2, Number: 5}},
		},
	}
	module := New(store)

	_, issue, err := module.LookupIssue(t.Context(), NumberRef{
		Owner:        "acme",
		Name:         "widget",
		Number:       5,
		PlatformHost: "ghe.example.com",
	})
	require.NoError(err)
	require.NotNil(issue)
	assert.EqualValues(202, issue.ID)
}

func TestResolveLocalItemTreatsMissingLocalRepoAsUnresolved(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	result, err := New(&fakeStore{}).ResolveLocalItem(t.Context(), NumberRef{
		Owner:        "acme",
		Name:         "widget",
		Number:       99,
		PlatformHost: "github.com",
	})
	require.NoError(err)
	assert.False(result.Found)
	assert.Nil(result.Repo)
}

type fakeStore struct {
	repos                     []db.Repo
	mrs                       map[int64]map[int]*db.MergeRequest
	issues                    map[int64]map[int]*db.Issue
	items                     map[int64]map[int]string
	listReposByOwnerNameCalls int
}

func (f *fakeStore) ListReposByOwnerName(
	_ context.Context,
	owner, name string,
) ([]db.Repo, error) {
	f.listReposByOwnerNameCalls++
	var repos []db.Repo
	for _, repo := range f.repos {
		if repo.Owner == owner && repo.Name == name {
			repos = append(repos, repo)
		}
	}
	return repos, nil
}

func (f *fakeStore) GetRepoByHostOwnerName(
	_ context.Context,
	platformHost, owner, name string,
) (*db.Repo, error) {
	for i := range f.repos {
		repo := &f.repos[i]
		if repo.PlatformHost == platformHost &&
			repo.Owner == owner &&
			repo.Name == name {
			return repo, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) GetMergeRequestByRepoIDAndNumber(
	_ context.Context,
	repoID int64,
	number int,
) (*db.MergeRequest, error) {
	if f.mrs == nil {
		return nil, nil
	}
	return f.mrs[repoID][number], nil
}

func (f *fakeStore) GetIssueByRepoIDAndNumber(
	_ context.Context,
	repoID int64,
	number int,
) (*db.Issue, error) {
	if f.issues == nil {
		return nil, nil
	}
	return f.issues[repoID][number], nil
}

func (f *fakeStore) ResolveItemNumber(
	_ context.Context,
	repoID int64,
	number int,
) (string, bool, error) {
	if f.items == nil {
		return "", false, nil
	}
	itemType := f.items[repoID][number]
	return itemType, itemType != "", nil
}
