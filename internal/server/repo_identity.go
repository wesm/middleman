package server

import (
	"context"

	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/repoidentity"
)

type repoIdentityRef struct {
	owner        string
	name         string
	platformHost string
}

type repositoryIdentityModule struct {
	module repoidentity.Module
}

type resolvedLocalItem = repoidentity.ResolvedLocalItem

var (
	errRepoAmbiguous           = repoidentity.ErrAmbiguous
	errRepoMissingPlatformHost = repoidentity.ErrMissingPlatformHost
)

func (s *Server) repoIdentity() repositoryIdentityModule {
	return repositoryIdentityModule{module: repoidentity.New(s.db)}
}

func (m repositoryIdentityModule) LookupRepo(
	ctx context.Context,
	ref repoIdentityRef,
) (*db.Repo, error) {
	return m.module.LookupRepo(ctx, repoidentity.Ref{
		Owner:        ref.owner,
		Name:         ref.name,
		PlatformHost: ref.platformHost,
	})
}

func (m repositoryIdentityModule) LookupRepoID(
	ctx context.Context,
	ref repoIdentityRef,
) (int64, error) {
	return m.module.LookupRepoID(ctx, repoidentity.Ref{
		Owner:        ref.owner,
		Name:         ref.name,
		PlatformHost: ref.platformHost,
	})
}

func (m repositoryIdentityModule) LookupMRID(
	ctx context.Context,
	ref repoNumberPathRef,
) (int64, error) {
	return m.module.LookupMRID(ctx, repoidentity.NumberRef{
		Owner:        ref.owner,
		Name:         ref.name,
		Number:       ref.number,
		PlatformHost: ref.platformHost,
	})
}

func (m repositoryIdentityModule) LookupIssueID(
	ctx context.Context,
	ref repoNumberPathRef,
) (int64, error) {
	return m.module.LookupIssueID(ctx, repoidentity.NumberRef{
		Owner:        ref.owner,
		Name:         ref.name,
		Number:       ref.number,
		PlatformHost: ref.platformHost,
	})
}

func (m repositoryIdentityModule) LookupIssue(
	ctx context.Context,
	ref repoNumberPathRef,
) (*db.Repo, *db.Issue, error) {
	return m.module.LookupIssue(ctx, repoidentity.NumberRef{
		Owner:        ref.owner,
		Name:         ref.name,
		Number:       ref.number,
		PlatformHost: ref.platformHost,
	})
}

func (m repositoryIdentityModule) ResolveLocalItem(
	ctx context.Context,
	ref repoNumberPathRef,
) (resolvedLocalItem, error) {
	return m.module.ResolveLocalItem(ctx, repoidentity.NumberRef{
		Owner:        ref.owner,
		Name:         ref.name,
		Number:       ref.number,
		PlatformHost: ref.platformHost,
	})
}
