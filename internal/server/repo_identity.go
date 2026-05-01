package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wesm/middleman/internal/db"
)

type repoIdentityRef struct {
	owner        string
	name         string
	platformHost string
}

type repoLookupMode int

const (
	repoLookupOwnerNameAllowed repoLookupMode = iota
	repoLookupRequireUnambiguousOwnerName
)

type repositoryIdentityModule struct {
	server *Server
}

type resolvedLocalItem struct {
	Repo     *db.Repo
	ItemType string
	Found    bool
}

var errRepoAmbiguous = errors.New("repo ambiguous")

func (s *Server) repoIdentity() repositoryIdentityModule {
	return repositoryIdentityModule{server: s}
}

func (m repositoryIdentityModule) LookupRepo(
	ctx context.Context,
	ref repoIdentityRef,
	mode repoLookupMode,
) (*db.Repo, error) {
	ref = normalizeRepoIdentityRef(ref)
	if ref.platformHost != "" {
		return m.lookupRepoOnHost(ctx, ref)
	}
	if mode == repoLookupRequireUnambiguousOwnerName {
		repos, err := m.server.db.ListReposByOwnerName(ctx, ref.owner, ref.name)
		if err != nil {
			return nil, fmt.Errorf("list matching repos: %w", err)
		}
		switch len(repos) {
		case 0:
			return nil, errRepoNotFound
		case 1:
			return &repos[0], nil
		default:
			return nil, errRepoAmbiguous
		}
	}

	repo, err := m.server.db.GetRepoByOwnerName(ctx, ref.owner, ref.name)
	if err != nil {
		return nil, fmt.Errorf("get repo: %w", err)
	}
	if repo == nil {
		return nil, errRepoNotFound
	}
	return repo, nil
}

func (m repositoryIdentityModule) LookupRepoID(
	ctx context.Context,
	ref repoIdentityRef,
	mode repoLookupMode,
) (int64, error) {
	repo, err := m.LookupRepo(ctx, ref, mode)
	if err != nil {
		return 0, err
	}
	return repo.ID, nil
}

func (m repositoryIdentityModule) LookupMRID(
	ctx context.Context,
	ref repoNumberPathRef,
) (int64, error) {
	repo, err := m.LookupRepo(ctx, repoIdentityRef{
		owner:        ref.owner,
		name:         ref.name,
		platformHost: ref.platformHost,
	}, repoLookupOwnerNameAllowed)
	if err != nil {
		return 0, err
	}

	mr, err := m.server.db.GetMergeRequestByRepoIDAndNumber(ctx, repo.ID, ref.number)
	if err != nil {
		return 0, err
	}
	if mr == nil {
		return 0, fmt.Errorf("MR %s/%s#%d not found", ref.owner, ref.name, ref.number)
	}
	return mr.ID, nil
}

func (m repositoryIdentityModule) LookupIssueID(
	ctx context.Context,
	ref repoNumberPathRef,
) (int64, error) {
	_, issue, err := m.LookupIssue(ctx, ref)
	if err != nil {
		return 0, err
	}
	return issue.ID, nil
}

func (m repositoryIdentityModule) LookupIssue(
	ctx context.Context,
	ref repoNumberPathRef,
) (*db.Repo, *db.Issue, error) {
	repo, err := m.LookupRepo(ctx, repoIdentityRef{
		owner:        ref.owner,
		name:         ref.name,
		platformHost: ref.platformHost,
	}, repoLookupOwnerNameAllowed)
	if err != nil {
		return nil, nil, err
	}
	issue, err := m.server.db.GetIssueByRepoIDAndNumber(ctx, repo.ID, ref.number)
	if err != nil {
		return nil, nil, err
	}
	if issue == nil {
		return repo, nil, fmt.Errorf(
			"issue %s/%s#%d not found", ref.owner, ref.name, ref.number,
		)
	}
	return repo, issue, nil
}

func (m repositoryIdentityModule) ResolveLocalItem(
	ctx context.Context,
	ref repoNumberPathRef,
) (resolvedLocalItem, error) {
	repo, err := m.LookupRepo(ctx, repoIdentityRef{
		owner:        ref.owner,
		name:         ref.name,
		platformHost: ref.platformHost,
	}, repoLookupOwnerNameAllowed)
	if errors.Is(err, errRepoNotFound) {
		return resolvedLocalItem{}, nil
	}
	if err != nil {
		return resolvedLocalItem{}, err
	}

	itemType, found, err := m.server.db.ResolveItemNumber(ctx, repo.ID, ref.number)
	if err != nil {
		return resolvedLocalItem{}, err
	}
	return resolvedLocalItem{
		Repo:     repo,
		ItemType: itemType,
		Found:    found,
	}, nil
}

func (m repositoryIdentityModule) lookupRepoOnHost(
	ctx context.Context,
	ref repoIdentityRef,
) (*db.Repo, error) {
	repo, err := m.server.db.GetRepoByHostOwnerName(
		ctx, ref.platformHost, ref.owner, ref.name,
	)
	if err != nil {
		return nil, fmt.Errorf("get repo: %w", err)
	}
	if repo == nil {
		return nil, errRepoNotFound
	}
	return repo, nil
}

func normalizeRepoIdentityRef(ref repoIdentityRef) repoIdentityRef {
	return repoIdentityRef{
		owner:        strings.ToLower(strings.TrimSpace(ref.owner)),
		name:         strings.ToLower(strings.TrimSpace(ref.name)),
		platformHost: strings.ToLower(strings.TrimSpace(ref.platformHost)),
	}
}
