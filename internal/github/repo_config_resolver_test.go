package github

import (
	"context"
	"errors"
	"testing"

	gh "github.com/google/go-github/v84/github"
	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/config"
)

func TestResolveConfiguredRepos_ExpandsGlobAndSkipsArchived(t *testing.T) {
	assert := Assert.New(t)
	client := &mockClient{
		listReposByOwnerFn: func(_ context.Context, owner string) ([]*gh.Repository, error) {
			return []*gh.Repository{
				{
					Name:     new("widgets"),
					Owner:    &gh.User{Login: new(owner)},
					Archived: new(false),
				},
				{
					Name:     new("widgets-api"),
					Owner:    &gh.User{Login: new(owner)},
					Archived: new(false),
				},
				{
					Name:     new("widgets-legacy"),
					Owner:    &gh.User{Login: new(owner)},
					Archived: new(true),
				},
			}, nil
		},
	}

	result := ResolveConfiguredRepos(
		context.Background(),
		map[string]Client{"github.com": client},
		[]config.Repo{{Owner: "acme", Name: "widgets-*"}},
	)

	require.Len(t, result.Configured, 1)
	assert.Equal(1, result.Configured[0].MatchedRepoCount)
	assert.Equal([]RepoRef{{
		Owner:        "acme",
		Name:         "widgets-api",
		PlatformHost: "github.com",
	}}, result.Expanded)
}

func TestResolveConfiguredRepos_DeduplicatesExactAndGlobMatches(t *testing.T) {
	assert := Assert.New(t)
	client := &mockClient{
		getRepositoryFn: func(
			_ context.Context, owner, repo string,
		) (*gh.Repository, error) {
			return &gh.Repository{
				Name:     new(repo),
				Owner:    &gh.User{Login: new(owner)},
				Archived: new(false),
			}, nil
		},
		listReposByOwnerFn: func(_ context.Context, owner string) ([]*gh.Repository, error) {
			return []*gh.Repository{
				{
					Name:     new("widgets"),
					Owner:    &gh.User{Login: new(owner)},
					Archived: new(false),
				},
				{
					Name:     new("widgets-api"),
					Owner:    &gh.User{Login: new(owner)},
					Archived: new(false),
				},
			}, nil
		},
	}

	result := ResolveConfiguredRepos(
		context.Background(),
		map[string]Client{"github.com": client},
		[]config.Repo{
			{Owner: "acme", Name: "widgets"},
			{Owner: "acme", Name: "widgets*"},
		},
	)

	assert.Len(result.Expanded, 2)
	assert.ElementsMatch([]RepoRef{
		{Owner: "acme", Name: "widgets", PlatformHost: "github.com"},
		{Owner: "acme", Name: "widgets-api", PlatformHost: "github.com"},
	}, result.Expanded)
}

func TestResolveConfiguredRepos_DeduplicatesOwnerCase(t *testing.T) {
	assert := Assert.New(t)
	client := &mockClient{
		getRepositoryFn: func(
			_ context.Context, owner, repo string,
		) (*gh.Repository, error) {
			return &gh.Repository{
				Name:     new(repo),
				Owner:    &gh.User{Login: new("acme")},
				Archived: new(false),
			}, nil
		},
		listReposByOwnerFn: func(_ context.Context, owner string) ([]*gh.Repository, error) {
			return []*gh.Repository{
				{
					Name:     new("widgets"),
					Owner:    &gh.User{Login: new("acme")},
					Archived: new(false),
				},
			}, nil
		},
	}

	result := ResolveConfiguredRepos(
		context.Background(),
		map[string]Client{"github.com": client},
		[]config.Repo{
			{Owner: "Acme", Name: "widgets"},
			{Owner: "acme", Name: "widgets*"},
		},
	)

	assert.Equal([]RepoRef{{
		Owner:        "acme",
		Name:         "widgets",
		PlatformHost: "github.com",
	}}, result.Expanded)
}

func TestResolveConfiguredReposCasefoldsResolvedRepoRefs(t *testing.T) {
	assert := Assert.New(t)
	client := &mockClient{
		getRepositoryFn: func(
			_ context.Context, _, _ string,
		) (*gh.Repository, error) {
			return &gh.Repository{
				Name:     new("Foo"),
				Owner:    &gh.User{Login: new("Org")},
				Archived: new(false),
			}, nil
		},
	}

	result := ResolveConfiguredRepos(
		context.Background(),
		map[string]Client{"github.com": client},
		[]config.Repo{{Owner: "org", Name: "foo"}},
	)

	assert.Equal([]RepoRef{{
		Owner:        "org",
		Name:         "foo",
		PlatformHost: "github.com",
	}}, result.Expanded)
}

func TestResolveConfiguredRepos_ReportsZeroCountOnStartupWarning(t *testing.T) {
	assert := Assert.New(t)
	client := &mockClient{
		listReposByOwnerFn: func(
			_ context.Context, owner string,
		) ([]*gh.Repository, error) {
			return nil, errors.New("boom")
		},
	}

	result := ResolveConfiguredRepos(
		context.Background(),
		map[string]Client{"github.com": client},
		[]config.Repo{{Owner: "acme", Name: "widgets-*"}},
	)

	require.Len(t, result.Configured, 1)
	assert.True(result.Configured[0].IsGlob)
	assert.Equal(0, result.Configured[0].MatchedRepoCount)
	assert.Empty(result.Expanded)
	assert.Len(result.Warnings, 1)
}

func TestResolveConfiguredRepos_MatchesRepoNamesCaseInsensitively(t *testing.T) {
	assert := Assert.New(t)
	client := &mockClient{
		listReposByOwnerFn: func(_ context.Context, owner string) ([]*gh.Repository, error) {
			return []*gh.Repository{
				{
					Name:     new("Widget-API"),
					Owner:    &gh.User{Login: new(owner)},
					Archived: new(false),
				},
			}, nil
		},
	}

	result := ResolveConfiguredRepos(
		context.Background(),
		map[string]Client{"github.com": client},
		[]config.Repo{{Owner: "acme", Name: "widget-*"}},
	)

	require.Len(t, result.Configured, 1)
	assert.Equal(1, result.Configured[0].MatchedRepoCount)
	assert.Equal([]RepoRef{{
		Owner:        "acme",
		Name:         "widget-api",
		PlatformHost: "github.com",
	}}, result.Expanded)
}
