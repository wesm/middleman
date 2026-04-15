package github

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/wesm/middleman/internal/config"
)

var ErrConfiguredRepoArchived = errors.New("configured repo archived")

func canonicalRepoName(name string) string {
	return strings.ToLower(name)
}

func canonicalRepoOwner(owner string) string {
	return strings.ToLower(owner)
}

func canonicalRepoHost(host string) string {
	if host == "" {
		host = "github.com"
	}
	return strings.ToLower(host)
}

func canonicalRepoRef(repo RepoRef) RepoRef {
	return RepoRef{
		Owner:        canonicalRepoOwner(repo.Owner),
		Name:         canonicalRepoName(repo.Name),
		PlatformHost: canonicalRepoHost(repo.PlatformHost),
	}
}

func canonicalRepoPattern(pattern string) string {
	return strings.ToLower(pattern)
}

type ConfiguredRepoStatus struct {
	Owner            string `json:"owner"`
	Name             string `json:"name"`
	IsGlob           bool   `json:"is_glob"`
	MatchedRepoCount int    `json:"matched_repo_count"`
}

type ResolveConfiguredReposResult struct {
	Configured []ConfiguredRepoStatus
	Expanded   []RepoRef
	Warnings   []error
}

func FallbackConfiguredRepoRefs(
	previous []RepoRef,
	raw config.Repo,
) []RepoRef {
	host := raw.PlatformHostOrDefault()
	if !raw.HasNameGlob() {
		for _, repo := range previous {
			if sameConfiguredRepoHost(repo.PlatformHost, host) &&
				strings.EqualFold(repo.Owner, raw.Owner) &&
				strings.EqualFold(repo.Name, raw.Name) {
				return []RepoRef{repo}
			}
		}
		return []RepoRef{{
			Owner:        canonicalRepoOwner(raw.Owner),
			Name:         canonicalRepoName(raw.Name),
			PlatformHost: canonicalRepoHost(host),
		}}
	}

	fallback := make([]RepoRef, 0)
	for _, repo := range previous {
		if !sameConfiguredRepoHost(repo.PlatformHost, host) ||
			!strings.EqualFold(repo.Owner, raw.Owner) {
			continue
		}
		matched, err := path.Match(
			canonicalRepoPattern(raw.Name),
			canonicalRepoName(repo.Name),
		)
		if err != nil || !matched {
			continue
		}
		fallback = append(fallback, repo)
	}
	return fallback
}

func ResolveConfiguredRepos(
	ctx context.Context,
	clients map[string]Client,
	repos []config.Repo,
) ResolveConfiguredReposResult {
	seen := make(map[string]struct{})
	result := ResolveConfiguredReposResult{
		Configured: make([]ConfiguredRepoStatus, 0, len(repos)),
	}

	for _, raw := range repos {
		status, expanded, err := resolveConfiguredRepo(
			ctx, clients, raw,
		)
		if err != nil {
			status.MatchedRepoCount = 0
			result.Warnings = append(result.Warnings, err)
		}
		result.Configured = append(result.Configured, status)
		for _, repo := range expanded {
			appendExpandedRepo(&result.Expanded, seen, repo)
		}
	}

	return result
}

func ResolveConfiguredRepo(
	ctx context.Context,
	clients map[string]Client,
	repo config.Repo,
) (ConfiguredRepoStatus, []RepoRef, error) {
	return resolveConfiguredRepo(ctx, clients, repo)
}

func resolveConfiguredRepo(
	ctx context.Context,
	clients map[string]Client,
	raw config.Repo,
) (ConfiguredRepoStatus, []RepoRef, error) {
	status := ConfiguredRepoStatus{
		Owner:  raw.Owner,
		Name:   raw.Name,
		IsGlob: raw.HasNameGlob(),
	}
	host := raw.PlatformHostOrDefault()
	client, ok := clients[host]
	if !ok {
		return status, nil, fmt.Errorf(
			"no client configured for host %s", host,
		)
	}

	if !status.IsGlob {
		repo, err := client.GetRepository(ctx, raw.Owner, raw.Name)
		if err != nil {
			return status, nil, fmt.Errorf(
				"resolve configured repo %s/%s: %w",
				raw.Owner, raw.Name, err,
			)
		}
		if repo.GetArchived() {
			return status, nil, fmt.Errorf(
				"%w: %s/%s",
				ErrConfiguredRepoArchived, raw.Owner, raw.Name,
			)
		}
		canonicalOwner := repo.GetOwner().GetLogin()
		if canonicalOwner == "" {
			canonicalOwner = raw.Owner
		}
		status.MatchedRepoCount = 1
		return status, []RepoRef{{
			Owner:        canonicalRepoOwner(canonicalOwner),
			Name:         canonicalRepoName(repo.GetName()),
			PlatformHost: canonicalRepoHost(host),
		}}, nil
	}

	repos, err := client.ListRepositoriesByOwner(ctx, raw.Owner)
	if err != nil {
		return status, nil, fmt.Errorf(
			"resolve configured repo glob %s/%s: %w",
			raw.Owner, raw.Name, err,
		)
	}

	matches := make([]RepoRef, 0, len(repos))
	for _, repo := range repos {
		if repo.GetArchived() {
			continue
		}
		matched, err := path.Match(
			canonicalRepoName(raw.Name),
			canonicalRepoName(repo.GetName()),
		)
		if err != nil {
			return status, nil, fmt.Errorf(
				"invalid repo glob %s/%s: %w",
				raw.Owner, raw.Name, err,
			)
		}
		if !matched {
			continue
		}
		canonicalOwner := repo.GetOwner().GetLogin()
		if canonicalOwner == "" {
			canonicalOwner = raw.Owner
		}
		matches = append(matches, RepoRef{
			Owner:        canonicalRepoOwner(canonicalOwner),
			Name:         canonicalRepoName(repo.GetName()),
			PlatformHost: canonicalRepoHost(host),
		})
	}
	status.MatchedRepoCount = len(matches)
	return status, matches, nil
}

func appendExpandedRepo(
	dst *[]RepoRef,
	seen map[string]struct{},
	repo RepoRef,
) {
	repo = canonicalRepoRef(repo)
	key := repo.PlatformHost + "\x00" + repo.Owner + "\x00" + repo.Name
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*dst = append(*dst, repo)
}

func sameConfiguredRepoHost(left, right string) bool {
	if left == "" {
		left = "github.com"
	}
	if right == "" {
		right = "github.com"
	}
	return strings.EqualFold(left, right)
}
