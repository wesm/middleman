package repoidentity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wesm/middleman/internal/db"
)

// Store is the persistence surface needed to resolve repository identity.
type Store interface {
	ListReposByOwnerName(ctx context.Context, owner, name string) ([]db.Repo, error)
	GetRepoByHostOwnerName(
		ctx context.Context,
		platformHost, owner, name string,
	) (*db.Repo, error)
	GetMergeRequestByRepoIDAndNumber(
		ctx context.Context,
		repoID int64,
		number int,
	) (*db.MergeRequest, error)
	GetIssueByRepoIDAndNumber(
		ctx context.Context,
		repoID int64,
		number int,
	) (*db.Issue, error)
	ResolveItemNumber(
		ctx context.Context,
		repoID int64,
		number int,
	) (itemType string, found bool, err error)
}

type Ref struct {
	Owner        string
	Name         string
	PlatformHost string
}

type NumberRef struct {
	Owner        string
	Name         string
	Number       int
	PlatformHost string
}

type ResolvedLocalItem struct {
	Repo     *db.Repo
	ItemType string
	Found    bool
}

var (
	ErrAmbiguous = errors.New("repo ambiguous")
	ErrNotFound  = errors.New("repo not found")
)

type Module struct {
	store Store
}

func New(store Store) Module {
	return Module{store: store}
}

func (m Module) LookupRepo(ctx context.Context, ref Ref) (*db.Repo, error) {
	ref = normalizeRef(ref)
	if ref.PlatformHost != "" {
		return m.lookupRepoOnHost(ctx, ref)
	}
	repos, err := m.store.ListReposByOwnerName(ctx, ref.Owner, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("list matching repos: %w", err)
	}
	switch len(repos) {
	case 0:
		return nil, ErrNotFound
	case 1:
		return &repos[0], nil
	default:
		return nil, ErrAmbiguous
	}
}

func (m Module) LookupRepoID(ctx context.Context, ref Ref) (int64, error) {
	repo, err := m.LookupRepo(ctx, ref)
	if err != nil {
		return 0, err
	}
	return repo.ID, nil
}

func (m Module) LookupMRID(ctx context.Context, ref NumberRef) (int64, error) {
	repo, err := m.LookupRepo(ctx, Ref{
		Owner:        ref.Owner,
		Name:         ref.Name,
		PlatformHost: ref.PlatformHost,
	})
	if err != nil {
		return 0, err
	}

	mr, err := m.store.GetMergeRequestByRepoIDAndNumber(ctx, repo.ID, ref.Number)
	if err != nil {
		return 0, err
	}
	if mr == nil {
		return 0, fmt.Errorf("MR %s/%s#%d not found", ref.Owner, ref.Name, ref.Number)
	}
	return mr.ID, nil
}

func (m Module) LookupIssueID(ctx context.Context, ref NumberRef) (int64, error) {
	_, issue, err := m.LookupIssue(ctx, ref)
	if err != nil {
		return 0, err
	}
	return issue.ID, nil
}

func (m Module) LookupIssue(
	ctx context.Context,
	ref NumberRef,
) (*db.Repo, *db.Issue, error) {
	repo, err := m.LookupRepo(ctx, Ref{
		Owner:        ref.Owner,
		Name:         ref.Name,
		PlatformHost: ref.PlatformHost,
	})
	if err != nil {
		return nil, nil, err
	}
	issue, err := m.store.GetIssueByRepoIDAndNumber(ctx, repo.ID, ref.Number)
	if err != nil {
		return nil, nil, err
	}
	if issue == nil {
		return repo, nil, fmt.Errorf(
			"issue %s/%s#%d not found", ref.Owner, ref.Name, ref.Number,
		)
	}
	return repo, issue, nil
}

func (m Module) ResolveLocalItem(
	ctx context.Context,
	ref NumberRef,
) (ResolvedLocalItem, error) {
	repo, err := m.LookupRepo(ctx, Ref{
		Owner:        ref.Owner,
		Name:         ref.Name,
		PlatformHost: ref.PlatformHost,
	})
	if errors.Is(err, ErrNotFound) {
		return ResolvedLocalItem{}, nil
	}
	if err != nil {
		return ResolvedLocalItem{}, err
	}

	itemType, found, err := m.store.ResolveItemNumber(ctx, repo.ID, ref.Number)
	if err != nil {
		return ResolvedLocalItem{}, err
	}
	return ResolvedLocalItem{
		Repo:     repo,
		ItemType: itemType,
		Found:    found,
	}, nil
}

func (m Module) lookupRepoOnHost(ctx context.Context, ref Ref) (*db.Repo, error) {
	repo, err := m.store.GetRepoByHostOwnerName(
		ctx, ref.PlatformHost, ref.Owner, ref.Name,
	)
	if err != nil {
		return nil, fmt.Errorf("get repo: %w", err)
	}
	if repo == nil {
		return nil, ErrNotFound
	}
	return repo, nil
}

func normalizeRef(ref Ref) Ref {
	return Ref{
		Owner:        strings.ToLower(strings.TrimSpace(ref.Owner)),
		Name:         strings.ToLower(strings.TrimSpace(ref.Name)),
		PlatformHost: strings.ToLower(strings.TrimSpace(ref.PlatformHost)),
	}
}
