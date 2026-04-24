package github

import (
	"time"

	gh "github.com/google/go-github/v84/github"
)

func ghTime(t time.Time) *gh.Timestamp { return &gh.Timestamp{Time: t} }

func ghRepo(owner, name string, archived bool) *gh.Repository {
	fullName := owner + "/" + name
	return &gh.Repository{
		Name:     new(name),
		FullName: new(fullName),
		Owner:    &gh.User{Login: new(owner)},
		Archived: new(archived),
	}
}

func ghLabel(id int64, name, description, color string, isDefault bool) *gh.Label {
	return &gh.Label{
		ID:          new(id),
		Name:        new(name),
		Description: new(description),
		Color:       new(color),
		Default:     new(isDefault),
	}
}

type ghWorkflowRunOpt func(*gh.WorkflowRun)

func ghWorkflowRun(id int64, headSHA string, opts ...ghWorkflowRunOpt) *gh.WorkflowRun {
	run := &gh.WorkflowRun{
		ID:      new(id),
		HeadSHA: new(headSHA),
		Event:   new("pull_request"),
	}
	for _, opt := range opts {
		opt(run)
	}
	return run
}

func withWorkflowPRNumbers(numbers ...int) ghWorkflowRunOpt {
	return func(run *gh.WorkflowRun) {
		run.PullRequests = make([]*gh.PullRequest, 0, len(numbers))
		for _, number := range numbers {
			run.PullRequests = append(run.PullRequests, &gh.PullRequest{Number: new(number)})
		}
	}
}

func withWorkflowHeadRepo(fullName string) ghWorkflowRunOpt {
	return func(run *gh.WorkflowRun) { run.HeadRepository = &gh.Repository{FullName: new(fullName)} }
}

func withWorkflowHeadBranch(branch string) ghWorkflowRunOpt {
	return func(run *gh.WorkflowRun) { run.HeadBranch = new(branch) }
}

type ghPROpt func(*gh.PullRequest)

func ghPR(number int, updatedAt time.Time, opts ...ghPROpt) *gh.PullRequest {
	pr := &gh.PullRequest{
		Number:    new(number),
		State:     new("open"),
		Title:     new("PR"),
		User:      &gh.User{Login: new("author")},
		CreatedAt: ghTime(updatedAt),
		UpdatedAt: ghTime(updatedAt),
		Head:      &gh.PullRequestBranch{Ref: new("feature"), SHA: new("head-sha")},
		Base:      &gh.PullRequestBranch{Ref: new("main"), SHA: new("base-sha")},
	}
	for _, opt := range opts {
		opt(pr)
	}
	return pr
}

func withPRHead(ref, sha string) ghPROpt {
	return func(pr *gh.PullRequest) { pr.Head.Ref, pr.Head.SHA = new(ref), new(sha) }
}

func withPRHeadRepo(fullName, cloneURL string) ghPROpt {
	return func(pr *gh.PullRequest) {
		pr.Head.Repo = &gh.Repository{FullName: new(fullName), CloneURL: new(cloneURL)}
	}
}

func withPRMerged(mergeCommitSHA string, at time.Time) ghPROpt {
	return func(pr *gh.PullRequest) {
		pr.Merged = new(true)
		pr.MergedAt = ghTime(at)
		pr.MergeCommitSHA = new(mergeCommitSHA)
	}
}
