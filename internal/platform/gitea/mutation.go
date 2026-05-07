package gitea

import (
	"context"

	giteasdk "code.gitea.io/sdk/gitea"
	"github.com/wesm/middleman/internal/platform"
	"github.com/wesm/middleman/internal/platform/gitealike"
)

func (c *Client) CreateMergeRequestComment(
	ctx context.Context,
	ref platform.RepoRef,
	number int,
	body string,
) (platform.MergeRequestEvent, error) {
	return c.provider.CreateMergeRequestComment(ctx, ref, number, body)
}

func (c *Client) EditMergeRequestComment(
	ctx context.Context,
	ref platform.RepoRef,
	commentID int64,
	body string,
) (platform.MergeRequestEvent, error) {
	return c.provider.EditMergeRequestComment(ctx, ref, commentID, body)
}

func (c *Client) CreateIssueComment(
	ctx context.Context,
	ref platform.RepoRef,
	number int,
	body string,
) (platform.IssueEvent, error) {
	return c.provider.CreateIssueComment(ctx, ref, number, body)
}

func (c *Client) EditIssueComment(
	ctx context.Context,
	ref platform.RepoRef,
	commentID int64,
	body string,
) (platform.IssueEvent, error) {
	return c.provider.EditIssueComment(ctx, ref, commentID, body)
}

func (c *Client) CreateIssue(
	ctx context.Context,
	ref platform.RepoRef,
	title string,
	body string,
) (platform.Issue, error) {
	return c.provider.CreateIssue(ctx, ref, title, body)
}

func (c *Client) SetMergeRequestState(
	ctx context.Context,
	ref platform.RepoRef,
	number int,
	state string,
) (platform.MergeRequest, error) {
	return c.provider.SetMergeRequestState(ctx, ref, number, state)
}

func (c *Client) SetIssueState(
	ctx context.Context,
	ref platform.RepoRef,
	number int,
	state string,
) (platform.Issue, error) {
	return c.provider.SetIssueState(ctx, ref, number, state)
}

func (c *Client) MergeMergeRequest(
	ctx context.Context,
	ref platform.RepoRef,
	number int,
	commitTitle string,
	commitMessage string,
	method string,
) (platform.MergeResult, error) {
	return c.provider.MergeMergeRequest(ctx, ref, number, commitTitle, commitMessage, method)
}

func (c *Client) ApproveMergeRequest(
	ctx context.Context,
	ref platform.RepoRef,
	number int,
	body string,
) (platform.MergeRequestEvent, error) {
	return c.provider.ApproveMergeRequest(ctx, ref, number, body)
}

func (c *Client) EditMergeRequestContent(
	ctx context.Context,
	ref platform.RepoRef,
	number int,
	title *string,
	body *string,
) (platform.MergeRequest, error) {
	return c.provider.EditMergeRequestContent(ctx, ref, number, title, body)
}

func (t *transport) CreateIssueComment(
	ctx context.Context,
	ref platform.RepoRef,
	number int,
	body string,
) (gitealike.CommentDTO, error) {
	comment, resp, err := t.api.CreateIssueComment(ref.Owner, ref.Name, int64(number), giteasdk.CreateIssueCommentOption{
		Body: body,
	})
	if err != nil {
		return gitealike.CommentDTO{}, giteaHTTPError(resp, err)
	}
	return convertComment(comment), nil
}

func (t *transport) EditIssueComment(
	ctx context.Context,
	ref platform.RepoRef,
	commentID int64,
	body string,
) (gitealike.CommentDTO, error) {
	comment, resp, err := t.api.EditIssueComment(ref.Owner, ref.Name, commentID, giteasdk.EditIssueCommentOption{
		Body: body,
	})
	if err != nil {
		return gitealike.CommentDTO{}, giteaHTTPError(resp, err)
	}
	return convertComment(comment), nil
}

func (t *transport) CreateIssue(
	ctx context.Context,
	ref platform.RepoRef,
	title string,
	body string,
) (gitealike.IssueDTO, error) {
	issue, resp, err := t.api.CreateIssue(ref.Owner, ref.Name, giteasdk.CreateIssueOption{
		Title: title,
		Body:  body,
	})
	if err != nil {
		return gitealike.IssueDTO{}, giteaHTTPError(resp, err)
	}
	return convertIssue(issue), nil
}

func (t *transport) EditIssue(
	ctx context.Context,
	ref platform.RepoRef,
	number int,
	opts gitealike.IssueMutationOptions,
) (gitealike.IssueDTO, error) {
	issue, resp, err := t.api.EditIssue(ref.Owner, ref.Name, int64(number), giteasdk.EditIssueOption{
		Title: stringValue(opts.Title),
		Body:  opts.Body,
		State: giteaStatePtr(opts.State),
	})
	if err != nil {
		return gitealike.IssueDTO{}, giteaHTTPError(resp, err)
	}
	return convertIssue(issue), nil
}

func (t *transport) EditPullRequest(
	ctx context.Context,
	ref platform.RepoRef,
	number int,
	opts gitealike.PullRequestMutationOptions,
) (gitealike.PullRequestDTO, error) {
	pr, resp, err := t.api.EditPullRequest(ref.Owner, ref.Name, int64(number), giteasdk.EditPullRequestOption{
		Title: stringValue(opts.Title),
		Body:  opts.Body,
		State: giteaStatePtr(opts.State),
	})
	if err != nil {
		return gitealike.PullRequestDTO{}, giteaHTTPError(resp, err)
	}
	return convertPullRequest(pr), nil
}

func (t *transport) MergePullRequest(
	ctx context.Context,
	ref platform.RepoRef,
	number int,
	opts gitealike.MergeOptions,
) (gitealike.MergeResultDTO, error) {
	merged, resp, err := t.api.MergePullRequest(ref.Owner, ref.Name, int64(number), giteasdk.MergePullRequestOption{
		Style:   giteaMergeStyle(opts.Method),
		Title:   opts.CommitTitle,
		Message: opts.CommitMessage,
	})
	if err != nil {
		return gitealike.MergeResultDTO{}, giteaHTTPError(resp, err)
	}
	return gitealike.MergeResultDTO{Merged: merged}, nil
}

func (t *transport) CreatePullReview(
	ctx context.Context,
	ref platform.RepoRef,
	number int,
	body string,
) (gitealike.ReviewDTO, error) {
	review, resp, err := t.api.CreatePullReview(ref.Owner, ref.Name, int64(number), giteasdk.CreatePullReviewOptions{
		State: giteasdk.ReviewStateApproved,
		Body:  body,
	})
	if err != nil {
		return gitealike.ReviewDTO{}, giteaHTTPError(resp, err)
	}
	return convertReview(review), nil
}

func giteaStatePtr(state *string) *giteasdk.StateType {
	if state == nil {
		return nil
	}
	value := giteasdk.StateType(*state)
	return &value
}

func giteaMergeStyle(method string) giteasdk.MergeStyle {
	switch method {
	case "squash":
		return giteasdk.MergeStyleSquash
	case "rebase":
		return giteasdk.MergeStyleRebase
	case "rebase-merge":
		return giteasdk.MergeStyleRebaseMerge
	case "fast-forward-only":
		return giteasdk.MergeStyleFastForwardOnly
	default:
		return giteasdk.MergeStyleMerge
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
