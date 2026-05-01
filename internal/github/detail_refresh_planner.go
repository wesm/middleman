package github

import (
	"context"
	"fmt"

	"github.com/wesm/middleman/internal/db"
)

const defaultGitHubHost = "github.com"

type detailRefreshPlanner struct {
	db *db.DB
}

type detailRefreshPlanInput struct {
	TrackedRepos []RepoRef
	WatchedMRs   []WatchedMR
}

type detailRefreshPlan struct {
	Items              []QueueItem
	PullRequestListErr error
	IssueListErr       error
}

func newDetailRefreshPlanner(database *db.DB) detailRefreshPlanner {
	return detailRefreshPlanner{db: database}
}

func (p detailRefreshPlanner) Build(
	ctx context.Context,
	input detailRefreshPlanInput,
) detailRefreshPlan {
	trackedRepos := detailTrackedRepoSet(input.TrackedRepos)
	watchedMRs := detailWatchedMRSet(input.WatchedMRs)

	plan := detailRefreshPlan{}
	prs, err := p.db.ListMergeRequests(
		ctx, db.ListMergeRequestsOpts{State: "open"},
	)
	if err != nil {
		plan.PullRequestListErr = err
		return plan
	}
	for _, pr := range prs {
		repo, rErr := p.db.GetRepoByID(ctx, pr.RepoID)
		if rErr != nil || repo == nil {
			continue
		}
		if !trackedRepos[detailRepoKey(repo.PlatformHost, repo.Owner, repo.Name)] {
			continue
		}
		plan.Items = append(plan.Items, QueueItem{
			Type:            QueueItemPR,
			RepoOwner:       repo.Owner,
			RepoName:        repo.Name,
			Number:          pr.Number,
			PlatformHost:    detailDefaultHost(repo.PlatformHost),
			UpdatedAt:       pr.UpdatedAt,
			DetailFetchedAt: pr.DetailFetchedAt,
			CIHadPending:    pr.CIHadPending,
			Starred:         pr.Starred,
			Watched:         watchedMRs[detailWatchedMRKey(repo.Owner, repo.Name, pr.Number)],
			IsOpen:          true,
		})
	}

	issues, err := p.db.ListIssues(
		ctx, db.ListIssuesOpts{State: "open"},
	)
	if err != nil {
		plan.IssueListErr = err
		return plan
	}
	for _, issue := range issues {
		repo, rErr := p.db.GetRepoByID(ctx, issue.RepoID)
		if rErr != nil || repo == nil {
			continue
		}
		if !trackedRepos[detailRepoKey(repo.PlatformHost, repo.Owner, repo.Name)] {
			continue
		}
		plan.Items = append(plan.Items, QueueItem{
			Type:            QueueItemIssue,
			RepoOwner:       repo.Owner,
			RepoName:        repo.Name,
			Number:          issue.Number,
			PlatformHost:    detailDefaultHost(repo.PlatformHost),
			UpdatedAt:       issue.UpdatedAt,
			DetailFetchedAt: issue.DetailFetchedAt,
			Starred:         issue.Starred,
			IsOpen:          true,
		})
	}

	return plan
}

func detailTrackedRepoSet(repos []RepoRef) map[string]bool {
	tracked := make(map[string]bool, len(repos))
	for _, repo := range repos {
		tracked[detailRepoKey(repo.PlatformHost, repo.Owner, repo.Name)] = true
	}
	return tracked
}

func detailWatchedMRSet(mrs []WatchedMR) map[string]bool {
	watched := make(map[string]bool, len(mrs))
	for _, mr := range mrs {
		watched[detailWatchedMRKey(mr.Owner, mr.Name, mr.Number)] = true
	}
	return watched
}

func detailRepoKey(host, owner, name string) string {
	return detailDefaultHost(host) + "\x00" + owner + "/" + name
}

func detailWatchedMRKey(owner, name string, number int) string {
	return fmt.Sprintf("%s/%s#%d", owner, name, number)
}

func detailDefaultHost(host string) string {
	if host == "" {
		return defaultGitHubHost
	}
	return host
}
