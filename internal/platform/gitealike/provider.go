package gitealike

import (
	"context"
	"errors"
	"fmt"

	"github.com/wesm/middleman/internal/platform"
)

const maxCollectedPages = 1000

type Provider struct {
	kind      platform.Kind
	host      string
	transport Transport
	options   Options
}

type Options struct {
	ReadActions bool
}

type Option func(*Options)

func WithReadActions() Option {
	return func(options *Options) {
		options.ReadActions = true
	}
}

func NewProvider(
	kind platform.Kind,
	host string,
	transport Transport,
	opts ...Option,
) *Provider {
	var options Options
	for _, opt := range opts {
		opt(&options)
	}
	return &Provider{
		kind:      kind,
		host:      host,
		transport: transport,
		options:   options,
	}
}

func (p *Provider) Platform() platform.Kind {
	return p.kind
}

func (p *Provider) Host() string {
	return p.host
}

func (p *Provider) Capabilities() platform.Capabilities {
	return platform.Capabilities{
		ReadRepositories:  true,
		ReadMergeRequests: true,
		ReadIssues:        true,
		ReadComments:      true,
		ReadReleases:      true,
		ReadCI:            true,
	}
}

func (p *Provider) GetRepository(
	ctx context.Context,
	ref platform.RepoRef,
) (platform.Repository, error) {
	repo, err := p.transport.GetRepository(ctx, ref.Owner, ref.Name)
	if err != nil {
		return platform.Repository{}, p.mapError(err)
	}
	return NormalizeRepository(p.kind, p.host, repo)
}

func (p *Provider) ListRepositories(
	ctx context.Context,
	owner string,
	opts platform.RepositoryListOptions,
) ([]platform.Repository, error) {
	repos, err := p.listRepositories(ctx, owner, p.transport.ListUserRepositories)
	if err != nil {
		if errors.Is(err, platform.ErrNotFound) {
			repos, err = p.listRepositories(ctx, owner, p.transport.ListOrgRepositories)
		}
	}
	if err != nil {
		return nil, err
	}
	if opts.Limit <= 0 && opts.Offset <= 0 {
		return repos, nil
	}
	return applyRepositoryListOptions(repos, opts), nil
}

func (p *Provider) ListOpenMergeRequests(
	ctx context.Context,
	ref platform.RepoRef,
) ([]platform.MergeRequest, error) {
	items, err := collectPages(ctx, func(opts PageOptions) ([]PullRequestDTO, Page, error) {
		return p.transport.ListOpenPullRequests(ctx, ref, opts)
	})
	if err != nil {
		return nil, p.mapError(err)
	}
	out := make([]platform.MergeRequest, 0, len(items))
	for _, item := range items {
		out = append(out, NormalizePullRequest(ref, item))
	}
	return out, nil
}

func (p *Provider) GetMergeRequest(
	ctx context.Context,
	ref platform.RepoRef,
	number int,
) (platform.MergeRequest, error) {
	pr, err := p.transport.GetPullRequest(ctx, ref, number)
	if err != nil {
		return platform.MergeRequest{}, p.mapError(err)
	}
	return NormalizePullRequest(ref, pr), nil
}

func (p *Provider) ListMergeRequestEvents(
	ctx context.Context,
	ref platform.RepoRef,
	number int,
) ([]platform.MergeRequestEvent, error) {
	comments, err := collectPages(ctx, func(opts PageOptions) ([]CommentDTO, Page, error) {
		return p.transport.ListPullRequestComments(ctx, ref, number, opts)
	})
	if err != nil {
		return nil, p.mapError(err)
	}
	reviews, err := collectPages(ctx, func(opts PageOptions) ([]ReviewDTO, Page, error) {
		return p.transport.ListPullRequestReviews(ctx, ref, number, opts)
	})
	if err != nil {
		return nil, p.mapError(err)
	}
	commits, err := collectPages(ctx, func(opts PageOptions) ([]CommitDTO, Page, error) {
		return p.transport.ListPullRequestCommits(ctx, ref, number, opts)
	})
	if err != nil {
		return nil, p.mapError(err)
	}
	return NormalizeMergeRequestEvents(p.kind, ref, number, comments, reviews, commits), nil
}

func (p *Provider) ListOpenIssues(
	ctx context.Context,
	ref platform.RepoRef,
) ([]platform.Issue, error) {
	items, err := collectPages(ctx, func(opts PageOptions) ([]IssueDTO, Page, error) {
		return p.transport.ListOpenIssues(ctx, ref, opts)
	})
	if err != nil {
		return nil, p.mapError(err)
	}
	out := make([]platform.Issue, 0, len(items))
	for _, item := range items {
		if item.IsPullRequest {
			continue
		}
		out = append(out, NormalizeIssue(ref, item))
	}
	return out, nil
}

func (p *Provider) GetIssue(
	ctx context.Context,
	ref platform.RepoRef,
	number int,
) (platform.Issue, error) {
	issue, err := p.transport.GetIssue(ctx, ref, number)
	if err != nil {
		return platform.Issue{}, p.mapError(err)
	}
	return NormalizeIssue(ref, issue), nil
}

func (p *Provider) ListIssueEvents(
	ctx context.Context,
	ref platform.RepoRef,
	number int,
) ([]platform.IssueEvent, error) {
	comments, err := collectPages(ctx, func(opts PageOptions) ([]CommentDTO, Page, error) {
		return p.transport.ListIssueComments(ctx, ref, number, opts)
	})
	if err != nil {
		return nil, p.mapError(err)
	}
	return NormalizeIssueComments(p.kind, ref, number, comments), nil
}

func (p *Provider) ListReleases(
	ctx context.Context,
	ref platform.RepoRef,
) ([]platform.Release, error) {
	items, err := collectPages(ctx, func(opts PageOptions) ([]ReleaseDTO, Page, error) {
		return p.transport.ListReleases(ctx, ref, opts)
	})
	if err != nil {
		return nil, p.mapError(err)
	}
	out := make([]platform.Release, 0, len(items))
	for _, item := range items {
		out = append(out, NormalizeRelease(ref, item))
	}
	return out, nil
}

func (p *Provider) ListTags(ctx context.Context, ref platform.RepoRef) ([]platform.Tag, error) {
	items, err := collectPages(ctx, func(opts PageOptions) ([]TagDTO, Page, error) {
		return p.transport.ListTags(ctx, ref, opts)
	})
	if err != nil {
		return nil, p.mapError(err)
	}
	out := make([]platform.Tag, 0, len(items))
	for _, item := range items {
		out = append(out, NormalizeTag(ref, item))
	}
	return out, nil
}

func (p *Provider) ListCIChecks(
	ctx context.Context,
	ref platform.RepoRef,
	sha string,
) ([]platform.CICheck, error) {
	statuses, err := collectPages(ctx, func(opts PageOptions) ([]StatusDTO, Page, error) {
		return p.transport.ListStatuses(ctx, ref, sha, opts)
	})
	if err != nil {
		return nil, p.mapError(err)
	}
	var actionRuns []ActionRunDTO
	if p.options.ReadActions {
		if actionsTransport, ok := p.transport.(ActionsTransport); ok {
			actionRuns, err = collectPages(ctx, func(opts PageOptions) ([]ActionRunDTO, Page, error) {
				return actionsTransport.ListActionRuns(ctx, ref, sha, opts)
			})
			if err != nil {
				return nil, p.mapError(err)
			}
		}
	}
	return NormalizeStatuses(ref, statuses, actionRuns), nil
}

func (p *Provider) listRepositories(
	ctx context.Context,
	owner string,
	list func(context.Context, string, PageOptions) ([]RepositoryDTO, Page, error),
) ([]platform.Repository, error) {
	items, err := collectPages(ctx, func(opts PageOptions) ([]RepositoryDTO, Page, error) {
		return list(ctx, owner, opts)
	})
	if err != nil {
		return nil, p.mapError(err)
	}
	out := make([]platform.Repository, 0, len(items))
	for _, item := range items {
		repo, err := NormalizeRepository(p.kind, p.host, item)
		if err != nil {
			return nil, err
		}
		if repo.Ref.Owner == owner {
			out = append(out, repo)
		}
	}
	return out, nil
}

func (p *Provider) mapError(err error) error {
	return mapTransportError(p.kind, p.host, err)
}

func collectPages[T any](
	ctx context.Context,
	fetch func(PageOptions) ([]T, Page, error),
) ([]T, error) {
	var out []T
	page := 1
	seen := make(map[int]bool)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if seen[page] {
			return nil, fmt.Errorf("gitealike pagination did not advance: page %d repeated", page)
		}
		if len(seen) >= maxCollectedPages {
			return nil, fmt.Errorf("gitealike pagination exceeded %d pages", maxCollectedPages)
		}
		seen[page] = true
		items, next, err := fetch(PageOptions{Page: page, PageSize: defaultPageSize})
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		nextPage := NextPage(next.Next)
		if nextPage == 0 {
			return out, nil
		}
		if nextPage <= page {
			return nil, fmt.Errorf("gitealike pagination did not advance: next page %d after page %d", nextPage, page)
		}
		page = nextPage
	}
}

func applyRepositoryListOptions(
	repos []platform.Repository,
	opts platform.RepositoryListOptions,
) []platform.Repository {
	start := max(opts.Offset, 0)
	if start >= len(repos) {
		return nil
	}
	end := len(repos)
	if opts.Limit > 0 && start+opts.Limit < end {
		end = start + opts.Limit
	}
	return repos[start:end]
}
